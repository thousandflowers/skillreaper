# Regenerate the README's measured block from a generated sample stack, so the
# hero numbers cannot drift from what reap actually prints.
#
# Idempotent by construction: the block between the two markers is replaced
# wholesale, so running the target twice leaves README.md byte-identical.
#
#   make readme-numbers
#   make readme-numbers && git diff --exit-code README.md   # must be clean
.PHONY: readme-numbers
readme-numbers:
	@set -eu; \
	dir="$$(mktemp -d)/hero-claude"; \
	docs/gif-helpers/hero-fixture.sh "$$dir" >/dev/null; \
	go run ./cmd/reap -claude-dir "$$dir" -claude-json "$$dir/.claude.json" \
		-no-nudge -agent > "$$dir/block.txt"; \
	test -s "$$dir/block.txt" || { echo "reap produced no output" >&2; exit 1; }; \
	grep -q '^<!-- readme-numbers:start -->$$' README.md || \
		{ echo "README.md has no readme-numbers markers — nothing to replace" >&2; exit 1; }; \
	awk -v f="$$dir/block.txt" ' \
		/^<!-- readme-numbers:start -->$$/ { \
			print; print "```"; \
			while ((getline l < f) > 0) print l; \
			print "```"; inside = 1; next } \
		/^<!-- readme-numbers:end -->$$/ { inside = 0 } \
		!inside { print }' README.md > "$$dir/README.md"; \
	cat "$$dir/README.md" > README.md; \
	echo "README.md: measured block regenerated"
