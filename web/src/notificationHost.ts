// Native modal dialogs require notifications inside their top-layer subtree.
// Ordinary appendChild moves restart CSS animations, including toast countdowns.
export function placeNotificationHost(
  host: HTMLElement,
  closingDialog?: HTMLDialogElement,
) {
  const dialogs = Array.from(
    document.querySelectorAll<HTMLDialogElement>("dialog[open]"),
  ).filter((dialog) => !closingDialog?.contains(dialog));
  const target = dialogs.at(-1) ?? document.body;
  if (host.parentElement === target) return;

  if (host.isConnected && typeof target.moveBefore === "function") {
    target.moveBefore(host, null);
    return;
  }

  // Preserve animation progress for browsers without state-preserving DOM moves.
  const animations = host.getAnimations({ subtree: true }).map((animation) => ({
    target: (animation.effect as KeyframeEffect | null)?.target,
    name: animation instanceof CSSAnimation ? animation.animationName : "",
    time: animation.currentTime,
    state: animation.playState,
    rate: animation.playbackRate,
  }));
  target.appendChild(host);
  for (const animation of host.getAnimations({ subtree: true })) {
    const previous = animations.find(
      (item) =>
        item.target === (animation.effect as KeyframeEffect | null)?.target &&
        item.name ===
          (animation instanceof CSSAnimation ? animation.animationName : ""),
    );
    if (!previous || previous.time === null) continue;
    animation.currentTime = previous.time;
    animation.playbackRate = previous.rate;
    if (previous.state === "paused") animation.pause();
  }
}

export function releaseNotificationHost(dialog: HTMLDialogElement) {
  // Move before React detaches the dialog, while animation state still exists.
  for (const host of dialog.querySelectorAll<HTMLElement>(".toast-host")) {
    placeNotificationHost(host, dialog);
  }
}
