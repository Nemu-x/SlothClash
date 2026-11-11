package sharelink

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// fragmentOptsFromFinalMask maps the first TCP "fragment" mask of an Xray fm
// parameter, {"tcp":[{"type":"fragment","settings":{"packets":"tlshello",
// "length":"10-20","delay":"0"}}]}, to the proxy's fragment-opts. Other masks
// are ignored.
//
// It mirrors core patch 0032 (core/patches/mihomo/0032-tls-hello-fragment.patch,
// common/convert/v.go fragmentOptsFromFinalMask) step for step, so a link
// imported here yields the same block the core's own share-link parser would:
// the same keys, string values, defaults and the same drop-not-fail rule for a
// mask that does not parse. Keep the two in sync.
func fragmentOptsFromFinalMask(fm string) map[string]any {
	if fm == "" {
		return nil
	}
	var finalMask struct {
		TCP []struct {
			Type     string                     `json:"type"`
			Settings map[string]json.RawMessage `json:"settings"`
		} `json:"tcp"`
	}
	if err := json.Unmarshal([]byte(fm), &finalMask); err != nil {
		return nil
	}
	for _, mask := range finalMask.TCP {
		if !strings.EqualFold(mask.Type, "fragment") {
			continue
		}
		opts := map[string]any{}
		for key, name := range map[string]string{"packets": "packets", "length": "length", "delay": "delay", "maxSplit": "max-split"} {
			raw, ok := mask.Settings[key]
			if !ok {
				continue
			}
			if v, ok := finalMaskValue(raw); ok {
				opts[name] = v
			}
		}
		// per-piece lists are not supported: take their first entry
		for key, name := range map[string]string{"lengths": "length", "delays": "delay"} {
			var list []json.RawMessage
			if _, ok := opts[name]; ok || json.Unmarshal(mask.Settings[key], &list) != nil || len(list) == 0 {
				continue
			}
			if v, ok := finalMaskValue(list[0]); ok {
				opts[name] = v
			}
		}
		if len(opts) == 0 {
			opts["packets"] = "tlshello"
		}
		// one bad link must not fail the whole subscription (nor this link):
		// the core refuses an invalid block at load, so drop it here
		packets, _ := opts["packets"].(string)
		length, _ := opts["length"].(string)
		delay, _ := opts["delay"].(string)
		maxSplit, _ := opts["max-split"].(string)
		if validateFragmentOpts(packets, length, delay, maxSplit) != nil {
			return nil
		}
		return opts
	}
	return nil
}

// finalMaskValue reads a JSON string or number as the string the core's
// fragment-opts fields take; numbers are truncated to an integer as in 0032.
func finalMaskValue(raw json.RawMessage) (string, bool) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	switch v := value.(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatInt(int64(v), 10), true
	}
	return "", false
}

// validateFragmentOpts is the check of the core's fragment.Options.Parse
// (0032, component/fragment/fragment.go): an all-empty block is valid (off),
// packets is "tlshello"/empty or a range from 1, length a range from 1
// (default 10-20), delay and max-split ranges from 0. A range is "N" or "N-M"
// (reversed bounds are swapped) within 0..math.MaxInt32.
func validateFragmentOpts(packets, length, delay, maxSplit string) error {
	if packets == "" && length == "" && delay == "" && maxSplit == "" {
		return nil
	}
	switch p := strings.ToLower(strings.TrimSpace(packets)); p {
	case "", "tlshello":
	default:
		lo, _, err := parseFragmentRange("packets", p, "")
		if err != nil {
			return err
		}
		if lo == 0 {
			return errors.New(`fragment-opts.packets: must be "tlshello" or a range starting from 1`)
		}
	}
	lo, _, err := parseFragmentRange("length", length, "10-20")
	if err != nil {
		return err
	}
	if lo == 0 {
		return errors.New("fragment-opts.length: minimum can't be 0")
	}
	if _, _, err := parseFragmentRange("delay", delay, "0"); err != nil {
		return err
	}
	_, _, err = parseFragmentRange("max-split", maxSplit, "0")
	return err
}

func parseFragmentRange(name, s, fallback string) (int, int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = fallback
	}
	lo, hi, found := strings.Cut(s, "-")
	minVal, err := strconv.Atoi(strings.TrimSpace(lo))
	if err != nil {
		return 0, 0, fmt.Errorf("fragment-opts.%s: invalid range %q", name, s)
	}
	maxVal := minVal
	if found {
		if maxVal, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil {
			return 0, 0, fmt.Errorf("fragment-opts.%s: invalid range %q", name, s)
		}
	}
	if maxVal < minVal {
		minVal, maxVal = maxVal, minVal
	}
	if minVal < 0 || maxVal > math.MaxInt32 {
		return 0, 0, fmt.Errorf("fragment-opts.%s: invalid range %q", name, s)
	}
	return minVal, maxVal, nil
}
