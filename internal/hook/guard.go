package hook

import "time"

// The name stays callable because installed accounts can still name it in
// Stop hooks. The service owns the context cycle; a Stop never holds it up.
func guardContext(_ string, _ []byte, _ time.Time) Reply { return Reply{} }
