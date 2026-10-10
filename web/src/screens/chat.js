import { html } from "../html.js";
import { isCodex } from "../agent.js";
import { ClaudeChat } from "./chat/claude.js";
import { CodexChat } from "./chat/codex.js";

// Chat opens the conversation of a session with the screen of its agent: both
// are the one conversation of chat/conversation.js, each with the parts its
// agent brings. A conversation with no live session left is read the way
// claude's is: nothing is done to it but reading.
export function Chat(props) {
    return isCodex(props.live)
        ? html`<${CodexChat} ...${props} />`
        : html`<${ClaudeChat} ...${props} />`;
}
