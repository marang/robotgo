#!/usr/bin/python3
"""Report only control geometry from the pinned KDE ScreenCast dialog."""

import sys
import time

import pyatspi


TIMEOUT_SECONDS = 30
POLL_SECONDS = 0.1
MINIMUM_CARD_EXTENT = 100


class LocatorError(RuntimeError):
    def __init__(self, stage):
        super().__init__(stage)
        self.stage = stage


def descendants(root):
    pending = [root]
    while pending:
        current = pending.pop()
        yield current
        try:
            pending.extend(reversed(list(current)))
        except (LookupError, RuntimeError):
            continue


def state_contains(accessible, state):
    try:
        return accessible.getState().contains(state)
    except (LookupError, RuntimeError):
        return False


def action(accessible):
    try:
        return accessible.queryAction()
    except (LookupError, NotImplementedError, RuntimeError):
        return None


def extents(accessible):
    try:
        rectangle = accessible.queryComponent().getExtents(
            pyatspi.DESKTOP_COORDS
        )
    except (LookupError, NotImplementedError, RuntimeError):
        return None
    if rectangle.width <= 0 or rectangle.height <= 0:
        return None
    return rectangle


def controls(root):
    output_cards = []
    disabled_buttons = []
    for accessible in descendants(root):
        rectangle = extents(accessible)
        if rectangle is None or not state_contains(
            accessible, pyatspi.STATE_SHOWING
        ):
            continue
        role = accessible.getRole()
        candidate_action = action(accessible)
        if (
            rectangle.width >= MINIMUM_CARD_EXTENT
            and rectangle.height >= MINIMUM_CARD_EXTENT
            and candidate_action is not None
        ):
            output_cards.append((rectangle.y, rectangle.x, accessible))
        if (
            role == pyatspi.ROLE_PUSH_BUTTON
            and candidate_action is not None
            and not state_contains(accessible, pyatspi.STATE_SENSITIVE)
        ):
            disabled_buttons.append(accessible)
    output_cards.sort(key=lambda item: (item[0], item[1]))
    return output_cards, disabled_buttons


def snapshot():
    desktop = pyatspi.Registry.getDesktop(0)
    try:
        windows = [
            window
            for application in desktop
            for window in application
        ]
    except (LookupError, RuntimeError):
        return []
    return [controls(window) for window in windows]


def wait_for_controls(deadline):
    last_candidates = []
    while time.monotonic() < deadline:
        candidates = snapshot()
        matching = [
            (cards, disabled)
            for cards, disabled in candidates
            if len(cards) == 2 and len(disabled) == 1
        ]
        if len(matching) == 1:
            return matching[0][0], matching[0][1][0]
        last_candidates = candidates
        time.sleep(POLL_SECONDS)
    maximum_cards = max(
        (len(cards) for cards, _ in last_candidates),
        default=0,
    )
    maximum_buttons = max(
        (len(disabled) for _, disabled in last_candidates),
        default=0,
    )
    if maximum_cards == 0:
        raise LocatorError("cards-0")
    if maximum_cards == 1:
        raise LocatorError("cards-1")
    if maximum_cards > 2:
        raise LocatorError("cards-many")
    if maximum_buttons == 0:
        raise LocatorError("buttons-0")
    if maximum_buttons > 1:
        raise LocatorError("buttons-many")
    raise LocatorError("dialog-ambiguous")


def center(accessible):
    rectangle = extents(accessible)
    if rectangle is None:
        raise LocatorError("controls-unavailable")
    return (
        rectangle.x + rectangle.width // 2,
        rectangle.y + rectangle.height // 2,
    )


def main():
    cards, confirmation = wait_for_controls(
        time.monotonic() + TIMEOUT_SECONDS
    )
    card_x, card_y = center(cards[1][2])
    button_x, button_y = center(confirmation)
    print("ok", card_x, card_y, button_x, button_y)


if __name__ == "__main__":
    try:
        main()
    except LocatorError as error:
        print("error", error.stage)
        sys.exit(1)
    except Exception:
        print("error accessibility-unavailable")
        sys.exit(1)
