// Pi adapter for the gate's agent step (go tool gate agent): what
// Claude Code gets from its hooks. Pi loads it once the project is trusted.
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const GATE_TIMEOUT_MS = 600_000;

export default function (pi: ExtensionAPI) {
	// What was last sent back to the model: the same failure is never sent twice
	// in a row, so a step that cannot run does not loop.
	let lastSent = "";

	const gate = (mode: string) => pi.exec("go", ["tool", "gate", "agent", mode], { timeout: GATE_TIMEOUT_MS });

	// A message typed by the user answers a pending simplify question.
	pi.on("input", async (event) => {
		if (event.source !== "extension") {
			await gate("prompt");
		}
		return { action: "continue" };
	});

	// Before Pi goes idle: a failing gate or a question for the user goes back
	// to the model for one more turn. The step asks once per tree, so this
	// cannot loop.
	pi.on("agent_before_settle", async (event) => {
		if (event.outcome !== "completed") {
			return;
		}

		const result = await gate("stop");
		if (result.code === 0) {
			return;
		}

		const content = [result.stdout, result.stderr].join("\n").trim();
		// A question is never a repeat: its text is the same for every tree,
		// and the gate already asks it only once per tree.
		if (!content.startsWith("QUESTION:") && content === lastSent) {
			return;
		}
		lastSent = content;

		return {
			entries: [...event.entries, { type: "custom_message", customType: "agent-gate", content, display: true }],
			continue: true,
		};
	});
}
