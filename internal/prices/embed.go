package prices

import _ "embed"

//go:embed api.json
var APIJSON []byte

//go:embed cursor.json
var CursorJSON []byte

//go:embed opencode.json
var OpenCodeJSON []byte
