1 Menu: no. No visual list of everything; can't tell if a group holds 1 or 100; open/close each time; a bit empty. LIKES: cleanliness.
2 Split: not bad. MISSING: how long/how many times unused; how much it actually bothers me; endless list to remove, so needs group selection. Colour scheme OK but pointless to repeat once inside a section.
3 Dashboard: no. Internal navigation not intuitive, hard; no order, just a heap. Only plus: shows better how many skills are installed, but fairly useless.
4 Table: not bad. Defects: cannot select whole groups; no data/detail on an item; wants to choose mute or prune for the single skill from there.
5 Guided: not bad. Dislikes: can't see how many remain (the "N of 395" counter did not register); can't see the list; can't prune or similar (first items are plugin ones, where p is hidden); does not understand how items get selected.
6 Inbox: dislikes: can't tell which are marked for removal vs which are useful; can't tell which one is currently selected (cursor too weak). LIKES: gradient bar to read the level.
7 Gauge: FAVOURITE so far. Too many data points, together with no order or purpose. WANTS: interactive, the dead-context bar drops as you remove things; Enter shows more data on a skill; quick way to decide on groups or single skills.
8 Map: visualisation not bad, but view-only: usability must improve a lot.
9 Calendar: no as main screen. Could be a small section of the menu; feels like a different menu altogether.
10 Receipt: LIKES a lot: seeing what I save (a few small errors), the line to strike out, the emotional feel. Dislikes: data too limited.
11 Proposal: hard to select what to remove; can't tell what the program suggests removing; can't see how much removing saves.
12 Proposal v2: still cannot tell which ones are recommended for removal.
13 Proposal v3: red badge too big and too red; confusing which are suggested and which are already set.
14 Proposal v4: user chooses as they go. Suggestion only in the skill's detail card, everything else disappears. LIKES tokens saved + cost. No "0,00"; cost in user's currency. Hold a key: see on screen what reap suggests vs what you did, different states. Shift+arrows: select many together.
v5: macchinoso, poco emotivo; space should prune; no whole-group selection; shift+arrows do not work.
v6: LIKES big number. Wants tokens too (subscription users care about tokens not money), toggled; by provider? Not clear that items and whole groups are selectable; shift+arrows do not work at all.
v7: provider = A (subscription or pay as you go, automatic). Going back from a skill card is counterintuitive. Must not show skillreaper itself: the tool must not delete itself. Other small errors, unspecified.
v8: on a skill card, up/down should move to the next skills in the list.
v8: skill names bigger in the card, to notice moving to another one.
v8: also wants prune/reap actions visible inside the card.
v8: shift still does not select. Wants the selection all light blue while selecting. Measured: Terminal.app sends plain up/down for shift+up/down; shift+left/right are distinct.
v8: big name wanted, but the double-line font is ugly: follow the UI style. Card menu (buttons) very ugly.
card: better, but moves too much and confuses when scrolling fast.
card: end of a section should show a full screen announcing the next list and continue. Shift still not working. Inner menu still not liked.
Shift+arrows via CGEventSourceFlagsState: user confirms it works and likes it. Card: up at the first item should go to the previous list.
card: must show whether the skill is selected and its fate (mute, prune, other).
card: liked name colour = weight. Fate needs another method.
space: hypothesis A confirmed (plugin agents/mcp cannot be selected and it was not visible). Card: unclear when it can be selected and whether it is selected.
cannot select from the full list any more (not reproduced in hidden session).
message seen: only Claude Code can disable it (cause 1: unselectable rows at the top).
card: action menu disliked, cannot select prune etc. well, not clear what can be done. Trying a left/right choice row.
plugins: A. every item has an action (agent mute, plugin MCP remove from config), group can run claude plugin disable, warns if used items inside. Update behaviour to verify.
card options must be exactly: nothing, protect, mute, prune, delete. Everything selectable.
delete must be red, with an alarm or warning signal.
mute on MCP: grey but reachable, with the reason explained underneath. Already muted skills show mute as active.
Missing a visible button/combo to actually execute. Box must show a letter by state (m when muted etc).
Two p (protect, prune) not OK: use synonyms. Now keep k, mute m, prune p, delete d, plugin off o.
keep and nothing too similar: keep becomes lock (l).
some groups could not be selected: those with no unused items. Now they act on all their items.
n must clear selections. The small graph in the list does not work or is not understood: now labelled USED with a percent, uses on items.
n must clear selections; list graph labelled. 
already muted: grey [m], chosen now: green (A, to try, user unsure).
apply is now shift+enter (read via CGEventSourceFlagsState, Terminal sends plain enter); a fills in reap's suggestion on the row, group or selection, never over a choice already made.
apply: A chosen for the group columns, one phrase. a and space now act on boxes already holding a state.
(m) removed: no marker for already muted, box shows only what you chose.
already muted: B, count on the group and the word muted on the row.
a pressed again undoes reap's choice and puts back what the boxes held.
reinvest: B, a step after the receipt, no key of its own in the menu.
list now grouped by suggestion first: suggest to prune / mute / not sure / lock / already muted. g cycles to by source and flat. a on keep sets lock.
clicks reached only groups with unused items; now every row acts like the keyboard.
click sets reap's suggestion, prune where it has none. Big number shows any currency symbol.
amounts use the currency set on the Mac (currency.py), rate fixed in rates.json from ECB.
receipt: only y, n, esc answer, arrows do nothing. After y: done line, then the reinvest offer.
reinvest is a credit, not a payment: removing earns, a new skill spends, tab switches tokens/currency. Trying combos by weight vs by topic (reinvest_views.py).
reinvest credit: combos name what you ALREADY removed, as a reward (earned by pruning: a + b), never a further action. No m, no earns.
enter opens the skill's page (skills.sh), space takes it. Written ones have no page.
reinvest source by need: online if something fits, else written. shift+enter adds what was taken, with a confirm.
reinvest install animation: rows run one by one with spinner and steps, bar fills, amount counts up, esc stops.
security line per proposal: reinvest's regex rules on the cached SKILL.md, not a guarantee. Laya not fit (reaction, relevance, duplicates), engine still to decide.
launch: skillreaper opens this menu; reap prune/mute stay on the command line with [y/N], --yes skips the question (A).

2026-11-05: a on "not sure, you decide" takes the same choice a click takes (own items prune, plugin skills and agents mute), so the whole group can be chosen at once. Read-only rows stay out. Space on a group works when its unused rows cannot be chosen.

2026-11-05: space, click and a all take reap's own choice; already muted rows change only by key: u undoes the mute (also a lock), p prunes.

2026-11-05: single plugin items stay unprunable (choice A): prune and delete off on them, the message says what works instead. Legend says space choose.
2026-11-06: rule: the user can choose any action on any group or single row (plugin items, links, shared folders); only reap's suggestions stay as they were, and the only refusals left are physical.

2026-11-06: reinvest keeps its own small memory of sessions (sessions_memory.json, about 1 KB each) because Claude Code deletes old transcripts.

2026-11-06: reinvest offers both ways for the same work: the closest online skill that is safe, with why it may not fit, and one written from your sessions. A security refusal is never offered.

2026-11-06: an already pruned group lists what is in quarantine; u brings it back. The word on screen for taking back a mute, lock or prune is undo.

2026-11-06: the line under the list adds up every choice by action in a fixed order (prune, mute, delete, plugin off, lock, undo); a refusal or warning takes its place until the next key.

2026-11-06: when a key on a group leaves rows out, the message says how many and why with the key that works instead, and those rows show a tag like cannot mute until the next key.

2026-11-06: the reinvest screen proposes only what Claude read and passed, best fit first, each with its reason in full, plus one written from your sessions. Skills that merely share a word are not shown.
