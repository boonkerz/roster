#!/usr/bin/env bash
# Gibt eine Version frei: verschiebt den Abschnitt „Unreleased“ beider Changelogs unter die
# neue Versionsnummer, schreibt VERSION, committet und setzt das Tag vX.Y.Z. Pusht NICHT –
# der Push (Branch + Tag) stößt den Release-Workflow an, der die Release-Notizen aus dem
# Changelog übernimmt. Aufruf: make release VERSION=0.17.0
set -euo pipefail
cd "$(dirname "$0")/.."

V="${1:-}"
[[ "$V" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "Nutzung: $0 X.Y.Z" >&2; exit 2; }
[ -z "$(git status --porcelain)" ] || { echo "Arbeitsverzeichnis nicht sauber." >&2; exit 1; }
[ "$(git rev-parse --abbrev-ref HEAD)" = "master" ] || { echo "Bitte auf master freigeben." >&2; exit 1; }
! git rev-parse -q --verify "refs/tags/v$V" >/dev/null || { echo "Tag v$V existiert schon." >&2; exit 1; }

# Unreleased-Abschnitt muss in beiden Dateien mindestens einen Eintrag haben.
unreleased() { awk '/^## \[Unreleased\]/{p=1; next} /^## \[/{p=0} p' "$1"; }
for f in CHANGELOG.md CHANGELOG.de.md; do
  grep -q '^## \[Unreleased\]' "$f" || { echo "$f: kein Abschnitt [Unreleased]." >&2; exit 1; }
  unreleased "$f" | grep -q '^- ' || { echo "$f: [Unreleased] ist leer." >&2; exit 1; }
  grep -q "^## \[$V\]" "$f" && { echo "$f: Abschnitt [$V] existiert schon." >&2; exit 1; }
done

TODAY="$(date +%F)"
PREV="$(git describe --tags --abbrev=0 --match 'v*' 2>/dev/null | sed 's/^v//')"
for f in CHANGELOG.md CHANGELOG.de.md; do
  python3 - "$f" "$V" "$TODAY" "$PREV" <<'PY'
import re, sys
f, v, today, prev = sys.argv[1:5]
s = open(f, encoding="utf-8").read()
s = s.replace("## [Unreleased]\n", f"## [Unreleased]\n\n## [{v}] - {today}\n", 1)
# Vergleichs-Links am Dateiende nachziehen.
s = re.sub(r"^\[Unreleased\]: .*$",
           f"[Unreleased]: https://github.com/boonkerz/roster/compare/v{v}...HEAD\n"
           f"[{v}]: https://github.com/boonkerz/roster/compare/v{prev}...v{v}" if prev else
           f"[Unreleased]: https://github.com/boonkerz/roster/compare/v{v}...HEAD\n"
           f"[{v}]: https://github.com/boonkerz/roster/releases/tag/v{v}",
           s, count=1, flags=re.M)
open(f, "w", encoding="utf-8").write(s)
PY
done
echo "$V" > VERSION

git add VERSION CHANGELOG.md CHANGELOG.de.md
git commit -q -m "Release $V" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
git tag "v$V"
echo "Release $V committet und getaggt. Veröffentlichen mit:"
echo "  git push && git push origin v$V"
