package stream

// LetterMessage is the input frame OpLetter gives claude. The live contract
// uses the same frame: a probe with its own copy can pass while the holder's
// letters wait forever for a person who is not there to accept them.
func LetterMessage(sessionID, id, text, mode string, argv []string) map[string]any {
	return letterMessage(sessionID, id, text, peerClass(mode, argv))
}

func letterMessage(sessionID, id, text, class string) map[string]any {
	return map[string]any{
		"type": "user", "session_id": sessionID, "parent_tool_use_id": nil, "uuid": id,
		"message": map[string]any{"role": "user", "content": text},
		"origin":  map[string]any{"kind": "peer", "from": "", "fromMode": class},
	}
}
