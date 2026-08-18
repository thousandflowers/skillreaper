# skillreaper - ASCII logotype variants

Eight wordmark options for `skillreaper` / `reap`.
All measured: max 58 columns, max 8 lines, 7-bit ASCII only, no color, no Unicode.
Each block below is copy-paste safe into a plain file, a GitHub issue or a log line.

---

## 1. Solid - condensed pixel

Baseline heavy wordmark. Full name on one line; 4-column glyphs are the widest that fit eleven letters inside 58.

```
#### #  # #### #    #    ###  ####  ##  ###  #### ###
#    # #   ##  #    #    #  # #    #  # #  # #    #  #
#### ##    ##  #    #    ###  ###  #### ###  ###  ###
   # # #   ##  #    #    # #  #    #  # #    #    # #
#### #  # #### #### #### #  # #### #  # #    #### #  #
```

- **Width:** 54 columns  
- **Height:** 5 lines  
- **Reads at 40 columns:** no (54 cols, wraps and breaks)  
- **Says:** It is a tool, not a toy. Reads as a compiled binary, no editorial.

## 2. Solid - weighted, two-tier

Two-thick strokes are only affordable at six letters, so REAPER carries the weight and SKILL is letterspaced above it as a qualifier.

```
S           K           I          L          L
######  #######   ###   ######  ####### ######
##   ## ##       ## ##  ##   ## ##      ##   ##
######  #####   ####### ######  #####   ######
##  ##  ##      ##   ## ##      ##      ##  ##
##   ## ####### ##   ## ##      ####### ##   ##
```

- **Width:** 47 columns  
- **Height:** 6 lines  
- **Reads at 40 columns:** no (47 cols, wraps and breaks)  
- **Says:** Hierarchy: the reaper is the product, skill is what it operates on.

## 3. Cut - diagonal slash

The scythe as a single continuous stroke entering bottom-left of the block and exiting top-right, cutting five letters. Each letter loses at most one row, so all eleven still read.

```
#### #  # #### #    #    ###  ####  ##  //#  #### ###
#    # #   ##  #    #    #  # #    # // #  # #    #  #
#### ##    ##  #    #    ###  ### //### ###  ###  ###
   # # #   ##  #    #    # #  #//  #  # #    #    # #
#### #  # #### #### #### #  //#### #  # #    #### #  #
```

- **Width:** 54 columns  
- **Height:** 5 lines  
- **Reads at 40 columns:** no (54 cols, wraps and breaks)  
- **Says:** Something passed through this word and took part of it with it.

## 4. Cut - pruned letter

The 'p' has already been reaped: same glyph, drawn in dots instead of solids. The shape survives so the word still reads, but the letter is visibly gone.

```
#### #  # #### #    #    ###  ####  ##  ...  #### ###
#    # #   ##  #    #    #  # #    #  # .  . #    #  #
#### ##    ##  #    #    ###  ###  #### ...  ###  ###
   # # #   ##  #    #    # #  #    #  # .    #    # #
#### #  # #### #### #### #  # #### #  # .    #### #  #
```

- **Width:** 54 columns  
- **Height:** 5 lines  
- **Reads at 40 columns:** no (54 cols, wraps and breaks)  
- **Says:** The thing you removed is still legible in outline - that is what pruning looks like.

## 5. Thin - single-stroke stick

Uppercase skeleton letterforms in pipes, underscores and slashes. Proportional widths, no filled mass anywhere.

```
 _  |  ___ |  |   _   _   _   _   _   _
|_  |/  |  |  |  |_) |_  |_| |_) |_  |_)
 _| |\ ___ |_ |_ | \ |_  | | |   |_  | \
```

- **Width:** 40 columns  
- **Height:** 3 lines  
- **Reads at 40 columns:** yes  
- **Says:** Quiet and instrumental - a label on a diagram rather than a banner.

## 6. Thin - schematic

Same pixel grid as variant 1, redrawn as an engineering drawing: corners as plus signs, runs as dashes and pipes.

```
+--+ |  + +--+ |    |    +--+ +--+ +--+ +--+ +--+ +--+
|    | /   ||  |    |    |  | |    |  | |  | |    |  |
+--+ |+    ||  |    |    +--+ +--+ +--+ +--+ +--+ +--+
   | | \   ||  |    |    | \  |    |  | |    |    | \
+--+ |  + +--+ +--+ +--+ |  + +--+ |  | |    +--+ |  +
```

- **Width:** 54 columns  
- **Height:** 5 lines  
- **Reads at 40 columns:** no (54 cols, wraps and breaks)  
- **Says:** A structure to be inspected and edited, which is what the tool does to your context.

## 7. reap - binary only

The four letters users actually type, at the largest weight the 58-column budget allows: nine-wide glyphs, two-thick strokes, six rows.

```
########   #########    #####    ########
##     ##  ##          ##   ##   ##     ##
##     ##  ######     ##     ##  ##     ##
########   ##         #########  ########
##    ##   ##         ##     ##  ##
##     ##  #########  ##     ##  ##
```

- **Width:** 42 columns  
- **Height:** 6 lines  
- **Reads at 40 columns:** no (42 cols, wraps and breaks)  
- **Says:** The command itself, with enough presence to head a --help screen.

## 8. Minimal - signature

Two lines. A plain lowercase wordmark with a rule under it whose last character turns up into the scythe.

```
skillreaper
----------/
```

- **Width:** 11 columns  
- **Height:** 2 lines  
- **Reads at 40 columns:** yes  
- **Says:** Almost not a logo: a wordmark that fits in a log line and still has one gesture in it.

---

## Recommendation

**Ship variant 3 (diagonal slash).** It is the only one that is both a real
wordmark and an argument: the block is heavy enough to head a README, and the
cut says what the tool does without a skull, a scythe glyph or any cliche. The
damage is bounded - one row per letter, five letters touched - so the word
still reads as `skillreaper` at a glance. Variant 4 makes the same point more
literally but a dotted letter reads as a rendering artifact in a log, and
variants 1 and 6 are correct but say nothing.

**For `reap --version`, use variant 8.** Version output gets piped, grepped and
pasted into bug reports; 11 columns and 2 lines survive all three, and a 54-column
banner in front of a version string is noise. If you want a banner anywhere in the
binary, put **variant 7** on `reap` with no arguments or at the top of `--help`,
where the user asked to look at the tool, and leave `--version` clean.
