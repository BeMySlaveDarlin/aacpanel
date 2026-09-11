// Package migrations holds the SQL schema migrations built into the binary.
//
// The files lie next to it and are named NNN_name.sql: the number sets the order
// and goes into the table schema_version, the name is for the human. There is
// one rule: an applied file is not edited, a change of schema is always a new
// number.
//
// They are embedded on purpose: the image is distroless, there is no deploy
// directory inside it, and the migrations have to travel together with the code
// that expects them.
//
// A stub for a future migration is named NNN_name.sql.wip. Such a file does not
// get in here and cannot be applied — which is exactly what makes a stub with
// the .sql extension dangerous: it is valid, it is applied with no effect, it
// records its version, and the real DDL is no longer applied once the file is
// filled in. The number stays taken meanwhile: TestMigrationsHaveUniqueNumbers
// reads the directory from disk and sees the stubs.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
