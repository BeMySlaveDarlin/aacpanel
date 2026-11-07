export const questionProse = "**Change plan**\nKeep `status_id 0`.\n\nChanges: 1) keep active deals. 2) unset empty status_id. 3) merge arrays.\n\nConfirm?";

export function readQuestion(root) {
    const el = root.querySelector(".asktext");
    const text = (node) => node?.textContent.replace(/\s+/g, " ").trim() || "";
    return {
        paragraphs: el?.querySelectorAll("p").length || 0,
        breaks: el?.querySelectorAll("br").length || 0,
        bold: [...(el?.querySelectorAll("b") || [])].map(text),
        code: [...(el?.querySelectorAll("code") || [])].map(text),
        items: [...(el?.querySelectorAll("ol > li") || [])].map(text),
    };
}
