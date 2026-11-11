#!/usr/bin/env python3
"""What the menu asks of the terminal: one whole key at a time, its own modes, a clean way in and out."""
import collections
import contextlib
import ctypes
import curses
import os
import re
import select
import signal
import sys
import termios
import threading
import time

Mouse = collections.namedtuple("Mouse", "x y kind")

SHIFT_ENTER = 0x11000  # above every curses key code
NON_LATIN = 0x11001  # a key whose letter is outside ASCII: no command can answer it
APPLY_KEY = ord("x")  # applies with no modifier, where Shift cannot be read
ENTER = (10, 13)
ESC = 27
SHIFT_GRACE_S = 0.3
ESC_WAIT_MS = 500 if os.environ.get("SSH_TTY") else 200  # an arrow split by a slow link must not read as Esc
POLL_MS = 250  # a blocking wait still notices a resume
SPIN_LIMIT = 50  # this many instant empty reads in a row means stdin is gone
CTRL_L = 12
BEL = 7
FOLD_EXEMPT = (ord("J"), ord("K"))  # shift+down and shift+up where the terminal cannot say otherwise

REMOTE_VARS = ("SSH_CONNECTION", "SSH_TTY")  # the keyboard the system would be asked about is not the one typing
S_KEYCODE = 1  # kVK_ANSI_S
CSI = {"A": curses.KEY_UP, "B": curses.KEY_DOWN, "C": curses.KEY_RIGHT, "D": curses.KEY_LEFT,
       "5~": curses.KEY_PPAGE, "6~": curses.KEY_NPAGE, "H": curses.KEY_HOME, "F": curses.KEY_END, "1~": curses.KEY_HOME, "4~": curses.KEY_END,
       "1;2A": curses.KEY_SR, "1;2B": curses.KEY_SF, "13;2u": SHIFT_ENTER, "27;2;13~": SHIFT_ENTER, "13u": 10}
SS3 = {"A": curses.KEY_UP, "B": curses.KEY_DOWN, "C": curses.KEY_RIGHT, "D": curses.KEY_LEFT, "M": 10, "H": curses.KEY_HOME, "F": curses.KEY_END}
STRING_OPENERS = tuple(map(ord, "]P_^X"))  # OSC, DCS, APC, PM, SOS: terminal replies, never keys
PASTE_START, PASTE_END = "200~", "\x1b[201~"

# Focus and the other mouse encodings may be left on by whatever ran before; drag needs 1002, wide windows 1006.
MOUSE_ON = "\033[?1002h\033[?1006h"
MOUSE_OFF = "\033[?1006l\033[?1002l\033[?1000l"
MODES_ON = f"\033[?1004l\033[?1005l\033[?1015l{MOUSE_ON}\033[?2004h"
MODES_OFF = f"\033[?2004l{MOUSE_OFF}"
BACKGROUND_QUERY = "\033]11;?\007"
BACKGROUND_WAIT_S = 0.1
BACKGROUND_REPLY = re.compile(rb"rgb:([0-9a-fA-F]+)/([0-9a-fA-F]+)/([0-9a-fA-F]+)")
LIGHT_FROM = 0.5  # share of full brightness
LIGHT_ANSI = (7, 15)  # the background numbers in COLORFGBG that are light


class Input:
    mouse = Mouse(0, 0, "other")
    last_key_at = 0.0
    gap = 1e9
    resumed = False
    mouse_on = True
    light = False


def _system_keyboard():
    """macOS reports the keyboard to any process, with no permission; Terminal hides Shift+up/down otherwise.
    (is Shift down, is the s key down, can it be asked at all)."""
    if any(os.environ.get(v) for v in REMOTE_VARS):
        return (lambda: False), (lambda: False), False
    try:
        q = ctypes.CDLL("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
        q.CGEventSourceFlagsState.restype = ctypes.c_uint64
        q.CGEventSourceFlagsState.argtypes = [ctypes.c_int]
        q.CGEventSourceKeyState.restype = ctypes.c_bool
        q.CGEventSourceKeyState.argtypes = [ctypes.c_int, ctypes.c_uint16]
        down = lambda: bool((q.CGEventSourceFlagsState(0) | q.CGEventSourceFlagsState(1)) & 0x20000)
        seen = [-SHIFT_GRACE_S]

        def watch():
            while True:
                if down():
                    seen[0] = time.monotonic()
                time.sleep(0.01)

        # The system's state can miss a quick Shift+Enter when asked only as the key arrives: remember it for a moment.
        threading.Thread(target=watch, daemon=True).start()
        held = lambda: down() or time.monotonic() - seen[0] < SHIFT_GRACE_S
        return held, (lambda: bool(q.CGEventSourceKeyState(0, S_KEYCODE))), True
    except (OSError, AttributeError):
        return (lambda: False), (lambda: False), False


SHIFT_HELD, S_HELD, SHIFT_READABLE = _system_keyboard()


def apply_key():
    """The key the footer may promise for applying: never one this terminal cannot deliver."""
    return "shift+enter" if SHIFT_READABLE else chr(APPLY_KEY)


def is_apply(k, shift_held, pending=True):
    """Shift+Enter however it arrives, or the plain key. Enter under a held Shift counts only with something to apply:
    a stuck modifier must not take away what Enter opens."""
    return (k == SHIFT_ENTER or (k == APPLY_KEY and not SHIFT_READABLE) or (k in ENTER and shift_held())) and pending


def last_mouse():
    return Input.mouse


def key_gap():
    """Seconds between the key just read and the one before it: a repeat or a paste leaves almost none."""
    return Input.gap


def _decode_mouse(code, x, y, released):
    """Only an unmodified left button paints: wheel, side buttons and ctrl+click are not a click."""
    plain_left = (code & 0b11) == 0 and not code & (4 | 8 | 16 | 64 | 128)
    if code & 64 and not code & (128 | 4 | 8 | 16) and code & 0b11 < 2:
        kind = "wheel_down" if code & 0b11 else "wheel_up"
    elif released or (code & 0b11) == 3 and not code & (64 | 128):
        kind = "release"
    elif plain_left:
        kind = "drag" if code & 32 else "press"
    else:
        kind = "other"
    Input.mouse = Mouse(x, y, kind)
    return curses.KEY_MOUSE


def _swallow_string(w):
    while True:
        b = _next(w)
        if b in (-1, BEL):
            return None
        if b == ESC:
            _next(w)
            return None


def _swallow_paste(w):
    tail = ""
    while not tail.endswith(PASTE_END):
        b = _next(w)
        if b == -1:
            return None
        tail = (tail + chr(b) if b < 256 else tail)[-len(PASTE_END):]
    return None


def _next(w):
    w.timeout(ESC_WAIT_MS)
    return w.getch()


def _csi(w):
    body = ""
    while True:
        b = _next(w)
        if b == -1 or b >= 256:
            return None
        body += chr(b)
        if 0x40 <= b <= 0x7e:
            break
    if body == "M":  # X10 mouse: three raw bytes follow
        code, x, y = _next(w), _next(w), _next(w)
        return None if -1 in (code, x, y) else _decode_mouse(code - 32, x - 33, y - 33, False)
    if body[0] == "<":  # SGR mouse
        try:
            code, x, y = map(int, body[1:-1].split(";"))
        except ValueError:
            return None
        return _decode_mouse(code, x - 1, y - 1, body[-1] == "m")
    if body == PASTE_START:
        return _swallow_paste(w)
    return CSI.get(body)


def _after_esc(w):
    """A key, or None for a whole sequence that means nothing here. A lone Esc comes back as Esc."""
    b = _next(w)
    if b == -1:
        return ESC
    if b == ord("["):
        return _csi(w)
    if b == ord("O"):
        final = _next(w)
        return SS3.get(chr(final)) if 0 <= final < 256 else None
    if b in STRING_OPENERS:
        return _swallow_string(w)
    if b in ENTER:
        return SHIFT_ENTER  # ESC CR: what Terminal sends for Shift+Return when a profile binds it
    return None  # Alt plus a key


def _swallow_utf8(w, lead):
    """The rest of one character, so that it reads as one key."""
    for _ in range(3 if lead >= 0xf0 else 2 if lead >= 0xe0 else 1 if lead >= 0xc0 else 0):
        _next(w)
    return NON_LATIN


def _fold(k):
    return k + 32 if ord("A") <= k <= ord("Z") and k not in FOLD_EXEMPT else k


def _resume(w):
    """After fg: the terminal may have new modes and a new size, and nobody told curses."""
    Input.resumed = False
    write_modes(MODES_ON if Input.mouse_on else MODES_ON + MOUSE_OFF)
    _disable_suspend_keys()
    try:
        size = os.get_terminal_size(1)
        if curses.is_term_resized(size.lines, size.columns):
            curses.resizeterm(size.lines, size.columns)
    except (OSError, ValueError, curses.error):
        pass
    w.clearok(True)


def read_key(w, timeout=-1):
    """One whole key. timeout in ms, -1 waits for ever; a timeout returns -1, a repaint request KEY_RESIZE."""
    deadline = None if timeout < 0 else time.monotonic() + timeout / 1000
    spins = 0
    try:
        while True:
            if Input.resumed:
                _resume(w)
                return curses.KEY_RESIZE
            wait = POLL_MS if deadline is None else max(0, min(POLL_MS, int((deadline - time.monotonic()) * 1000)))
            w.timeout(wait)
            started = time.monotonic()
            k = w.getch()
            if k == -1:
                if deadline is not None and time.monotonic() >= deadline:
                    return -1
                spins = spins + 1 if wait and time.monotonic() - started < wait / 2000 else 0
                if spins >= SPIN_LIMIT:
                    raise EOFError("stdin closed")
                continue
            spins = 0
            if 0x80 <= k < 0x100:
                k = _swallow_utf8(w, k)
            if k == ESC:
                k = _after_esc(w)
                if k is None:
                    continue
            Input.gap, Input.last_key_at = time.monotonic() - Input.last_key_at, time.monotonic()
            if k == CTRL_L:
                w.clearok(True)
                return curses.KEY_RESIZE
            return 10 if k == curses.KEY_ENTER else _fold(k)
    finally:
        w.timeout(-1)


def write_modes(modes):
    try:
        os.write(1, modes.encode())
    except OSError:
        pass


@contextlib.contextmanager
def without_mouse():
    """A screen with no use for the mouse hands it back: a drag selects its text, as anywhere in the terminal."""
    was = Input.mouse_on
    Input.mouse_on = False
    write_modes(MOUSE_OFF)
    try:
        yield
    finally:
        Input.mouse_on = was
        if was:
            write_modes(MOUSE_ON)


def _disable_suspend_keys():
    """Ctrl-Y suspends on macOS, one stray modifier from the y that confirms; Ctrl-S freezes the screen until Ctrl-Q,
    which reads as a dead program. endwin restores both."""
    try:
        attrs = termios.tcgetattr(0)
        attrs[0] &= ~termios.IXON
        if hasattr(termios, "VDSUSP"):
            attrs[6][termios.VDSUSP] = bytes([os.fpathconf(0, "PC_VDISABLE") & 0xff])
        termios.tcsetattr(0, termios.TCSANOW, attrs)
    except (termios.error, OSError, ValueError):
        pass


def _on_resume(*_):
    Input.resumed = True


def _on_fatal(signum, _):
    raise SystemExit(128 + signum)  # unwinds through leave() and curses.wrapper, so the shell gets its terminal back


def enter(w):
    w.keypad(False)  # read_key parses every sequence, so nothing depends on what terminfo knows
    write_modes(MODES_ON)
    _disable_suspend_keys()
    signal.signal(signal.SIGCONT, _on_resume)
    for s in (signal.SIGQUIT, signal.SIGHUP):
        signal.signal(s, _on_fatal)
    curses.flushinp()  # keys typed before launch and replies to old queries are not commands


def leave():
    curses.flushinp()  # what is left must not run in the shell
    write_modes(MODES_OFF)


def parse_background(reply):
    """True or False from a terminal's answer to the background query, None when it is no such answer."""
    found = BACKGROUND_REPLY.search(reply)
    if not found:
        return None
    shares = [int(part, 16) / (16 ** len(part) - 1) for part in found.groups()]
    return (0.299 * shares[0] + 0.587 * shares[1] + 0.114 * shares[2]) >= LIGHT_FROM


def _ask_background():
    """The terminal's own answer, read before curses starts. A late one is swallowed later as any other reply."""
    saved = termios.tcgetattr(0)
    raw = termios.tcgetattr(0)
    raw[3] &= ~(termios.ICANON | termios.ECHO)
    try:
        termios.tcsetattr(0, termios.TCSANOW, raw)
        write_modes(BACKGROUND_QUERY)
        if select.select([0], [], [], BACKGROUND_WAIT_S)[0]:
            return parse_background(os.read(0, 64))
        return None
    finally:
        termios.tcsetattr(0, termios.TCSANOW, saved)


def light_background():
    """Whether the colours chosen for a dark window would vanish here. Unknown reads as dark."""
    told = os.environ.get("COLORFGBG", "").rsplit(";", 1)[-1]
    if told.isdigit():
        return int(told) in LIGHT_ANSI
    try:
        return _ask_background() is True
    except (OSError, termios.error, ValueError):
        return False


def is_light():
    return Input.light


def problem():
    """Why this cannot run as a full-screen menu, or None."""
    if not (os.isatty(0) and os.isatty(1)):
        return "it needs an interactive terminal on both stdin and stdout"
    term = os.environ.get("TERM")
    if not term:
        return "TERM is not set"
    try:
        curses.setupterm(term, 1)
    except curses.error:
        return f"this system has no terminfo entry for TERM={term}"
    if not curses.tigetstr("cup"):
        return f"TERM={term} cannot place text on the screen"
    return None


def launch(main, *args):
    """Refuses in one line rather than with a traceback."""
    why = problem()
    if why:
        sys.exit(f"cannot open the menu: {why}")
    Input.light = light_background()
    for size_var in ("LINES", "COLUMNS"):  # exported ones would hide the real window and every resize after it
        os.environ.pop(size_var, None)
    curses.wrapper(main, *args)
