package main

import "regexp"

// profileIDPattern matches a well-formed, server-minted profile id
// (`profile-<timestamp>`, see app.go where ids are created). Frontend-supplied
// ids that reach filesystem paths (runtime/<id>/…) MUST match this so a crafted
// id such as "../../x" can never traverse out of the runtime dir (audit A6-1).
var profileIDPattern = regexp.MustCompile(`^profile-[0-9]+$`)

// validProfileID reports whether id is a safe, well-formed profile id.
func validProfileID(id string) bool {
	return profileIDPattern.MatchString(id)
}
