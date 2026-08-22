// Package pin gates a routed check on the toolchain pin its target
// directory declares. It reads the pin from mise.toml, resolves the
// toolchain version the check would actually use — from PATH on the
// subprocess route, from the Go release that built this binary on the
// in-process route — and compares the two with a satisfies rule (equal or
// newer), never an exact-string match. It also rejects a target whose own
// manifest (go.mod, Cargo.toml, or pyproject.toml) declares a floor above
// the pin, since a build could then use a toolchain the project itself
// says it can't rely on.
//
// Check is the package's one entry point. A nil result means the pin is
// satisfied and the caller runs the tool as normal; a non-nil result is a
// ready-to-emit class-20 (gate_negative) clikit.Result explaining why the
// caller should not.
package pin
