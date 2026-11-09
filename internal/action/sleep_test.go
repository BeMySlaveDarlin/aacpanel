package action

import "testing"

func TestFreezeAndThawRequireTheirLiveConversation(t *testing.T) {
	for _, kind := range []Kind{SessionFreeze, SessionThaw} {
		for _, conversation := range []string{"", "not-a-conversation", "66666666-6666-4666-8666-666666666666"} {
			r := Request{ID: "sleep", Kind: kind, Target: "lab", Conversation: conversation}
			if err := r.Validate(); (err == nil) != safeUUID(conversation) {
				t.Errorf("%s conversation %q: %v", kind, conversation, err)
			}
		}
	}
}
