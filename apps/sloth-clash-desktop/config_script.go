package main

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// The per-profile JavaScript override (openspec/changes/js-script-override).
//
// A profile may carry a `function main(config, ctx) { ...; return config }` that
// transforms the runtime config after our overlays and before final validation.
// Everything in here is built around one rule: a script may reshape the config,
// but it must never be able to hang, crash, or block a connect. Every failure
// path returns the ORIGINAL config plus a reported error.

const (
	// maxProfileScriptBytes caps the stored script so state.json cannot be
	// bloated without bound. Enforced on both the save path and the run path —
	// a script that somehow got past save is still refused before compiling.
	maxProfileScriptBytes = 256 * 1024

	// profileScriptTimeout is the wall-clock budget for one invocation.
	// Generous for a config transform and invisible next to core startup; a
	// script that hits it is reported as a timeout, never as a hang.
	profileScriptTimeout = 3 * time.Second

	// profileScriptMaxCallStack bounds runaway recursion. goja's default is
	// unbounded, which turns `function f(){f()}` into a process-killing stack
	// overflow instead of a script error.
	profileScriptMaxCallStack = 2048

	// Console capture bounds. A script that logs in a loop must cost us a
	// bounded buffer and nothing more.
	profileScriptMaxConsoleLines = 200
	profileScriptMaxConsoleBytes = 16 * 1024

	// profileScriptName is the filename goja reports in errors and stack traces.
	profileScriptName = "profile-script.js"

	// profileScriptEntryPoint is the single documented entry point.
	profileScriptEntryPoint = "main"
)

// errProfileScriptTimeout is handed to vm.Interrupt so an interrupted run is
// distinguishable from any other failure.
var errProfileScriptTimeout = errors.New("script timeout")

// scriptContext is the read-only second argument handed to main(config, ctx).
// This is API we have to keep, so it stays deliberately small: only facts a
// script cannot derive from the config itself and would otherwise fight our
// overlays over.
type scriptContext struct {
	// Traffic is "tun" or "proxy" — the mode this config is being generated
	// for. Without it a script cannot tell which of our two overlay shapes it
	// is looking at.
	Traffic string `json:"traffic"`
	// Platform is the GOOS the app is running on ("windows", "darwin", "linux").
	Platform string `json:"platform"`
	// AppVersion is the desktop app version string.
	AppVersion string `json:"appVersion"`
}

// scriptResult reports what happened, for the UI, the log, and the preview pane.
type scriptResult struct {
	// Ran is true when a script was actually executed (false for the empty-script
	// short circuit, where no engine is constructed at all).
	Ran bool `json:"ran"`
	// Applied is true when the script's config was accepted and used.
	Applied bool `json:"applied"`
	// Err is the human-readable failure reason, empty on success.
	Err string `json:"error,omitempty"`
	// Line/Column locate the failure when the engine reports a position (1-based,
	// 0 when unknown).
	Line   int `json:"line,omitempty"`
	Column int `json:"column,omitempty"`
	// Console holds captured console.log/warn/error output, oldest first.
	Console []string `json:"console,omitempty"`
	// ConsoleTruncated marks that output was dropped to stay inside the buffer.
	ConsoleTruncated bool `json:"consoleTruncated,omitempty"`
	// DurationMS is the wall-clock time the script ran for.
	DurationMS int64 `json:"durationMs,omitempty"`
}

// failedScriptResult builds the common "discard the output, report why" result.
func failedScriptResult(base scriptResult, format string, args ...any) scriptResult {
	base.Applied = false
	base.Err = fmt.Sprintf(format, args...)
	return base
}

// scriptConsole is the bounded sink behind console.log/warn/error.
type scriptConsole struct {
	lines     []string
	bytes     int
	truncated bool
}

func (c *scriptConsole) push(level, text string) {
	if c.truncated {
		return
	}
	line := text
	if level != "log" {
		line = level + ": " + text
	}
	if len(c.lines) >= profileScriptMaxConsoleLines || c.bytes+len(line) > profileScriptMaxConsoleBytes {
		c.truncated = true
		return
	}
	c.lines = append(c.lines, line)
	c.bytes += len(line)
}

// installScriptConsole wires console.log/warn/error into the bounded buffer.
// This is the ONLY host object the sandbox gets: goja ships no require, fetch,
// setTimeout, process or fs, and we add nothing else (asserted by tests).
func installScriptConsole(vm *goja.Runtime, sink *scriptConsole) error {
	console := vm.NewObject()
	for _, level := range []string{"log", "warn", "error", "info", "debug"} {
		lvl := level
		fn := func(call goja.FunctionCall) goja.Value {
			parts := make([]string, 0, len(call.Arguments))
			for _, a := range call.Arguments {
				parts = append(parts, stringifyForConsole(vm, a))
			}
			sink.push(lvl, strings.Join(parts, " "))
			return goja.Undefined()
		}
		if err := console.Set(lvl, fn); err != nil {
			return err
		}
	}
	return vm.Set("console", console)
}

// stringifyForConsole renders a console argument. Objects go through JSON so a
// script author sees their structure instead of "[object Object]"; anything JSON
// refuses (cycles, functions) falls back to the engine's own string form.
func stringifyForConsole(vm *goja.Runtime, v goja.Value) string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return v.String()
	}
	if obj, ok := v.(*goja.Object); ok {
		if jsonVal := vm.Get("JSON"); jsonVal != nil {
			if jsonObj, ok := jsonVal.(*goja.Object); ok {
				if stringify, ok := goja.AssertFunction(jsonObj.Get("stringify")); ok {
					if out, err := stringify(goja.Undefined(), obj); err == nil && out != nil && !goja.IsUndefined(out) {
						return out.String()
					}
				}
			}
		}
	}
	return v.String()
}

// runProfileScript executes the profile's script against cfg and returns the
// config to continue the pipeline with.
//
// Contract: it NEVER returns nil, and on any failure it returns cfg unchanged
// (the caller keeps generating from the pre-script config) together with a
// result carrying the reason. The script mutates a deep copy, so a script that
// fails half-way through mutating cannot leave the real config in a torn state.
func runProfileScript(script string, cfg map[string]any, ctx scriptContext) (result map[string]any, res scriptResult) {
	trimmed := strings.TrimSpace(script)
	if trimmed == "" {
		// Empty script short circuit: no engine is constructed at all, so a
		// profile without a script costs exactly nothing.
		return cfg, scriptResult{}
	}
	res = scriptResult{Ran: true}
	if len(script) > maxProfileScriptBytes {
		return cfg, failedScriptResult(res, "script is %d bytes, over the %d byte limit", len(script), maxProfileScriptBytes)
	}

	// A panic inside the engine must degrade like any other script failure.
	defer func() {
		if r := recover(); r != nil {
			result = cfg
			res = failedScriptResult(res, "script engine panicked: %v", r)
		}
	}()

	prog, err := goja.Compile(profileScriptName, script, false)
	if err != nil {
		line, col := scriptErrorPosition(err)
		res.Line, res.Column = line, col
		return cfg, failedScriptResult(res, "syntax error: %s", scriptErrorMessage(err))
	}

	vm := goja.New()
	vm.SetMaxCallStackSize(profileScriptMaxCallStack)
	sink := &scriptConsole{}
	if err := installScriptConsole(vm, sink); err != nil {
		return cfg, failedScriptResult(res, "could not prepare the script sandbox: %v", err)
	}
	defer func() {
		res.Console = sink.lines
		res.ConsoleTruncated = sink.truncated
	}()

	timer := time.AfterFunc(profileScriptTimeout, func() { vm.Interrupt(errProfileScriptTimeout) })
	defer timer.Stop()

	started := time.Now()
	defer func() { res.DurationMS = time.Since(started).Milliseconds() }()

	if _, err := vm.RunProgram(prog); err != nil {
		return cfg, scriptRunFailure(res, err, "script failed while loading")
	}

	entry := vm.Get(profileScriptEntryPoint)
	if entry == nil || goja.IsUndefined(entry) || goja.IsNull(entry) {
		return cfg, failedScriptResult(res, "script defines no %s(config) function", profileScriptEntryPoint)
	}
	fn, ok := goja.AssertFunction(entry)
	if !ok {
		return cfg, failedScriptResult(res, "%s is not a function", profileScriptEntryPoint)
	}

	// The script works on a deep copy: whatever it mutates in place cannot reach
	// the caller's map unless the run succeeds and we convert the return value.
	cfgJS, err := configValueToJS(vm, deepCopyConfigValue(cfg))
	if err != nil {
		return cfg, failedScriptResult(res, "could not hand the config to the script: %v", err)
	}
	ctxJS, err := configValueToJS(vm, map[string]any{
		"traffic":    ctx.Traffic,
		"platform":   ctx.Platform,
		"appVersion": ctx.AppVersion,
	})
	if err != nil {
		return cfg, failedScriptResult(res, "could not hand the context to the script: %v", err)
	}

	out, err := fn(goja.Undefined(), cfgJS, ctxJS)
	if err != nil {
		// No prefix here: the UI already says "Script failed at line:col", so
		// "main() failed: Error: test" would read as a stutter. The thrown
		// value alone is what the author needs.
		return cfg, scriptRunFailure(res, err, "")
	}

	converted, err := jsValueToConfigValue(out, "config", make(map[*goja.Object]bool))
	if err != nil {
		return cfg, failedScriptResult(res, "%v", err)
	}
	next, ok := converted.(map[string]any)
	if !ok {
		return cfg, failedScriptResult(res, "%s() must return an object, got %s", profileScriptEntryPoint, describeReturn(converted))
	}

	res.Applied = true
	return next, res
}

// scriptRunFailure maps an engine error onto a reported failure, keeping the
// timeout distinguishable and extracting a position when goja provides one.
func scriptRunFailure(res scriptResult, err error, prefix string) scriptResult {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		if val := interrupted.Value(); val == errProfileScriptTimeout || errors.Is(err, errProfileScriptTimeout) {
			return failedScriptResult(res, "script exceeded the %s time limit", profileScriptTimeout)
		}
		return failedScriptResult(res, "script was interrupted: %v", interrupted.Value())
	}
	var stackOverflow *goja.StackOverflowError
	if errors.As(err, &stackOverflow) {
		return failedScriptResult(res, "script recursed too deeply (call stack limit %d)", profileScriptMaxCallStack)
	}
	line, col := scriptErrorPosition(err)
	res.Line, res.Column = line, col
	if prefix == "" {
		return failedScriptResult(res, "%s", scriptErrorMessage(err))
	}
	return failedScriptResult(res, "%s: %s", prefix, scriptErrorMessage(err))
}

// describeReturn names what a script returned, for the "must return an object"
// error, without dumping the whole value into the message.
func describeReturn(v any) string {
	switch t := v.(type) {
	case nil:
		return "null/undefined"
	case []any:
		return "an array"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case int, int64, float64:
		return "a number"
	default:
		return fmt.Sprintf("%T", t)
	}
}

// scriptPositionRe pulls "Line 3:12" (compiler) or ":3:12" (stack frame) out of
// a goja error. goja exposes positions only through its message/stack text, so
// this is the pragmatic way to give the editor something to point at.
var scriptPositionRe = regexp.MustCompile(`(?:Line |:)(\d+):(\d+)`)

func scriptErrorPosition(err error) (int, int) {
	if err == nil {
		return 0, 0
	}
	text := err.Error()
	var exception *goja.Exception
	if errors.As(err, &exception) {
		// The stack carries the throwing frame; the bare message usually does not.
		text = exception.String()
	}
	m := scriptPositionRe.FindStringSubmatch(text)
	if len(m) != 3 {
		return 0, 0
	}
	line := atoiSafe(m[1])
	col := atoiSafe(m[2])
	return line, col
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
		if n > 1<<20 {
			return 0
		}
	}
	return n
}

// scriptErrorMessage renders an engine error for a human: the thrown value's
// message for exceptions (without goja's Go-side stack noise), the plain text
// otherwise.
func scriptErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var exception *goja.Exception
	if errors.As(err, &exception) {
		if val := exception.Value(); val != nil {
			return strings.TrimSpace(val.String())
		}
	}
	msg := strings.TrimSpace(err.Error())
	if idx := strings.Index(msg, "\n"); idx > 0 {
		msg = strings.TrimSpace(msg[:idx])
	}
	return msg
}

// deepCopyConfigValue copies the config so the script cannot mutate the caller's
// map through the objects we hand it.
func deepCopyConfigValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = deepCopyConfigValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = deepCopyConfigValue(val)
		}
		return out
	default:
		return v
	}
}

// configValueToJS builds NATIVE JavaScript objects and arrays for the script.
//
// Deliberately not vm.ToValue(map) on the Go map: that hands the script a
// Go-backed wrapper where Array.isArray is false and push/splice/filter behave
// unlike real arrays — config scripts are mostly array surgery on proxies,
// rules and groups, so they need the real thing.
func configValueToJS(vm *goja.Runtime, v any) (goja.Value, error) {
	switch t := v.(type) {
	case nil:
		return goja.Null(), nil
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return vm.ToValue(t), nil
	case map[string]any:
		return configMapToJS(vm, t)
	case []any:
		return configSliceToJS(vm, t)
	}

	// Anything else goes through reflection rather than vm.ToValue, because our
	// own pipeline puts typed containers in the config — `dns-hijack` is a
	// []string, corp-VPN merges produce []any, subscriptions can bring
	// map[string]string. Handing those to vm.ToValue would wrap the Go value
	// instead of creating a JS array/object: Array.isArray would be false and
	// the round trip back would produce an object with numeric keys.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		items := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			jsVal, err := configValueToJS(vm, rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			items = append(items, jsVal)
		}
		return vm.NewArray(items...), nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("config contains a map keyed by %s, which has no JavaScript equivalent", rv.Type().Key())
		}
		obj := vm.NewObject()
		iter := rv.MapRange()
		for iter.Next() {
			jsVal, err := configValueToJS(vm, iter.Value().Interface())
			if err != nil {
				return nil, err
			}
			if err := obj.Set(iter.Key().String(), jsVal); err != nil {
				return nil, err
			}
		}
		return obj, nil
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return goja.Null(), nil
		}
		return configValueToJS(vm, rv.Elem().Interface())
	default:
		return vm.ToValue(v), nil
	}
}

func configMapToJS(vm *goja.Runtime, m map[string]any) (goja.Value, error) {
	obj := vm.NewObject()
	for k, val := range m {
		jsVal, err := configValueToJS(vm, val)
		if err != nil {
			return nil, err
		}
		if err := obj.Set(k, jsVal); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

func configSliceToJS(vm *goja.Runtime, s []any) (goja.Value, error) {
	items := make([]any, 0, len(s))
	for _, val := range s {
		jsVal, err := configValueToJS(vm, val)
		if err != nil {
			return nil, err
		}
		items = append(items, jsVal)
	}
	return vm.NewArray(items...), nil
}

// jsValueToConfigValue converts what the script returned back into the
// map[string]any the pipeline speaks.
//
// This is the sharp edge of the whole feature: goja hands every JS number back
// as a float64 once arithmetic touched it, and `mixed-port: 7890.0` is a
// different config than `mixed-port: 7890`. Integral floats are therefore
// normalized to int, recursively, and anything YAML cannot represent is refused
// by path instead of being written out as garbage.
func jsValueToConfigValue(v goja.Value, path string, seen map[*goja.Object]bool) (any, error) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil, nil
	}

	if obj, ok := v.(*goja.Object); ok {
		if seen[obj] {
			return nil, fmt.Errorf("value at %s is a cyclic reference, which cannot be written to the config", path)
		}
		seen[obj] = true
		defer delete(seen, obj)

		switch class := obj.ClassName(); class {
		case "Array":
			length := obj.Get("length")
			n := 0
			if length != nil {
				n = int(length.ToInteger())
			}
			out := make([]any, 0, n)
			for i := 0; i < n; i++ {
				item, err := jsValueToConfigValue(obj.Get(fmt.Sprintf("%d", i)), fmt.Sprintf("%s[%d]", path, i), seen)
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			}
			return out, nil
		case "Object":
			// ClassName alone is not enough: goja reports Map, Set, WeakMap and
			// Promise as plain "Object" too, and every one of them has no
			// enumerable own keys — they would serialize into `{}` and silently
			// delete whatever the user meant to write. The constructor name is
			// the reliable discriminator.
			if ctor := objectConstructorName(obj); ctor != "" && ctor != "Object" {
				return nil, fmt.Errorf("value at %s is a %s, which cannot be written to the config", path, ctor)
			}
			keys := obj.Keys()
			out := make(map[string]any, len(keys))
			for _, k := range keys {
				val, err := jsValueToConfigValue(obj.Get(k), path+"."+k, seen)
				if err != nil {
					return nil, err
				}
				out[k] = val
			}
			return out, nil
		case "Function":
			return nil, fmt.Errorf("value at %s is a function, which cannot be written to the config", path)
		default:
			// Date, RegExp, Error, ... — all of these would either vanish
			// silently or serialize into something mihomo cannot read. Refusing
			// by name is honest.
			return nil, fmt.Errorf("value at %s is a %s, which cannot be written to the config", path, class)
		}
	}

	exported := v.Export()
	switch t := exported.(type) {
	case string, bool:
		return t, nil
	case int64:
		return int(t), nil
	case int:
		return t, nil
	case float64:
		return normalizeConfigFloat(t, path)
	case float32:
		return normalizeConfigFloat(float64(t), path)
	case *goja.Symbol:
		return nil, fmt.Errorf("value at %s is a symbol, which cannot be written to the config", path)
	default:
		return nil, fmt.Errorf("value at %s has unsupported type %T", path, exported)
	}
}

// objectConstructorName reports the name of the object's constructor ("Object"
// for a plain literal, "Map"/"Set"/"Promise"/"Uint8Array"/... for the builtins
// that masquerade as plain objects, "" for a prototype-less object).
func objectConstructorName(obj *goja.Object) string {
	ctorVal := obj.Get("constructor")
	if ctorVal == nil || goja.IsUndefined(ctorVal) || goja.IsNull(ctorVal) {
		return ""
	}
	ctor, ok := ctorVal.(*goja.Object)
	if !ok {
		return ""
	}
	nameVal := ctor.Get("name")
	if nameVal == nil || goja.IsUndefined(nameVal) || goja.IsNull(nameVal) {
		return ""
	}
	return nameVal.String()
}

// normalizeConfigFloat turns an integral float back into an int (7890.0 → 7890)
// and refuses the non-finite values YAML has no representation for.
func normalizeConfigFloat(f float64, path string) (any, error) {
	if math.IsNaN(f) {
		return nil, fmt.Errorf("value at %s is NaN, which cannot be written to the config", path)
	}
	if math.IsInf(f, 0) {
		return nil, fmt.Errorf("value at %s is Infinity, which cannot be written to the config", path)
	}
	if f == math.Trunc(f) && f >= math.MinInt64 && f <= math.MaxInt64 {
		return int(int64(f)), nil
	}
	return f, nil
}

// applyProfileScriptStep is steps 4a + 4b of finalizeRuntimeConfigPipeline: run
// the profile's script over the post-overlay config, then re-assert the
// invariants the app depends on.
//
// Failure policy (openspec: "Script failures degrade instead of blocking"):
// whatever goes wrong, m is left holding a usable config — either the script's
// output or, on any failure, exactly what the pipeline produced without it —
// and the reason travels back to the caller. A broken script must cost the user
// a visible error, never a connect.
func applyProfileScriptStep(
	m map[string]any,
	script, traffic string,
	mixedPort, ctrlPort int,
	secret string,
	withExternalController bool,
	enableTun bool,
) scriptResult {
	if strings.TrimSpace(script) == "" {
		// No engine is constructed, and the invariants are already in place from
		// the overlay — a profile without a script pays nothing for this step.
		return scriptResult{}
	}

	next, res := runProfileScript(script, m, scriptContext{
		Traffic:    traffic,
		Platform:   runtime.GOOS,
		AppVersion: AppVersion,
	})
	if res.Applied && next != nil {
		replaceConfigMapContents(m, next)
	} else if res.Err != "" {
		debugLog("config", "JS-1", "config_script.go:applyProfileScriptStep",
			"profile script failed; generating without it",
			map[string]any{"error": res.Err, "line": res.Line, "column": res.Column, "durationMs": res.DurationMS})
	}

	// 4b — unconditional: after a successful run because the script may have
	// rewritten them, after a failed one because it costs nothing and keeps the
	// two paths identical.
	reassertCriticalRuntimeInvariants(m, mixedPort, ctrlPort, secret, withExternalController, enableTun)
	return res
}

// replaceConfigMapContents swaps the contents of dst for src in place, because
// the pipeline threads one map by reference through every stage.
func replaceConfigMapContents(dst, src map[string]any) {
	for k := range dst {
		delete(dst, k)
	}
	for k, v := range src {
		dst[k] = v
	}
}
