// Control requests and follower feedback (docs/ui-redesign.md §5.6, §5.7):
// the "<name> asks for control" toast for whoever may hand over, and the
// "Typing is off while you follow" toast for a follower who types.
//
// Feedback only. Nothing here writes to a PTY: the one thing it sends is a
// control message (grant, take or request control) through the caller's
// sendSessionJSON, the same messages the Share popover's buttons send.
// Keystrokes stay gated by term.js (isDriving, ptyWritesBlocked), and the
// runtime refuses writes from anyone but the driver.
//   sendSessionJSON(pane, msg)  sends a control message on the pane's session
//   toast(message)              the plain notice for any other control event
//   isDriving, isOwner, isTermPane(pane)
import * as Share from "../lib/share.js";
import { createActionToasts } from "./actiontoast.js";

export function createShareControl({ doc = document, sendSessionJSON, toast, isDriving, isOwner, isTermPane }) {
  const controlToasts = createActionToasts({ doc, className: "join-prompts", label: "Share requests" });
  const followToasts = createActionToasts({ doc, className: "k-toasts--center", label: "Following" });

  // noteControl handles a session "control" event. A request (someone asks
  // for control) becomes a toast for whoever may hand over (the driver or
  // the owner: the runtime's GrantControl rule); Hand over sends the same
  // grant-control message as Pass control. Anything else is the old toast.
  function noteControl(pane, msg) {
    if (Share.isControlRequest(msg) && msg.fromId !== pane.you?.id && (isDriving(pane) || isOwner(pane))) {
      const name = Share.clientName(pane.attached, msg.fromId);
      controlToasts.show({
        key: `${pane.runtimeId}\n${msg.fromId}`,
        tone: "haze",
        role: "alertdialog",
        icon: Share.glyph("steering"),
        title: `${name} asks for control`,
        desc: `Of ${pane.title || "this session"}. You follow until ${isOwner(pane) ? "you take it back" : "it is passed back"}.`,
        actions: [
          { label: "Hand over", primary: true, run: () => sendSessionJSON(pane, { type: "grant-control", targetId: msg.fromId }) },
          { label: "Keep driving" },
        ],
      });
      return;
    }
    if (!Share.isControlRequest(msg)) controlToasts.dismissWhere(`${pane.runtimeId}\n`);
    toast(msg.message);
  }

  // A follower who types gets told why nothing happens. Feedback only: it
  // listens in the capture phase and never stops the event, so xterm and
  // wirePaneTerminal (isDriving, ptyWritesBlocked) still decide, as before.
  function wireFollowerNotice(pane, wrap = pane.wrap) {
    wrap?.addEventListener(
      "keydown",
      (ev) => {
        if (!Share.isTypingKey(ev)) return;
        if (!isTermPane(pane) || !pane.you || isDriving(pane)) return;
        if (ev.target?.closest?.(".term-find, .term-input, .k-pane-banner, .pane-bar")) return;
        const owner = isOwner(pane);
        followToasts.show({
          key: `typing\n${pane.id}`,
          tone: "haze",
          icon: Share.glyph("lock"),
          title: "Typing is off while you follow",
          timeout: 4000,
          actions: [
            owner
              ? { label: "Take control", run: () => sendSessionJSON(pane, { type: "take-control" }) }
              : { label: "Ask for control", run: () => sendSessionJSON(pane, { type: "request-control" }) },
          ],
        });
      },
      true,
    );
  }

  // forgetSession drops the control requests shown for a session whose
  // share stopped.
  function forgetSession(runtimeId) {
    controlToasts.dismissWhere(`${runtimeId}\n`);
  }

  // followFocus keeps "Typing is off" only on the focused pane, and only
  // while it follows: focusedFollowerId is that pane's id, or "".
  function followFocus(focusedFollowerId) {
    followToasts.dismissWhere("typing\n", focusedFollowerId ? `typing\n${focusedFollowerId}` : "");
  }

  return { noteControl, wireFollowerNotice, forgetSession, followFocus };
}
