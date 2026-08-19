from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one replacement, found {count}: {old[:120]!r}")
    p.write_text(text.replace(old, new, 1))


# The native-media unsupported-method path records a capability downgrade from
# telegram.go, so the production adapter must import the capability package.
replace_once(
    "pkg/channels/telegram/telegram.go",
    '"github.com/As-tsaqib/picoclaw/pkg/bus"\n\t"github.com/As-tsaqib/picoclaw/pkg/channels"',
    '"github.com/As-tsaqib/picoclaw/pkg/bus"\n\t"github.com/As-tsaqib/picoclaw/pkg/capability"\n\t"github.com/As-tsaqib/picoclaw/pkg/channels"',
)

# Compare migration output with the actual normalized legacy entry. The normal
# CuratedStore write path is authoritative for provenance normalization; the
# migration contract is to preserve that stored metadata, not the caller's raw
# pre-normalization mutation fields.
replace_once(
    "pkg/memory/curated_migration_hardening_test.go",
    '''\tif _, err := seed.ApplyBatch(CuratedTargetCurrentUser, legacyCaller, []CuratedMutation{{
\t\tAction: CuratedActionPin, ID: seedResult.Applied[0].ID,
\t}}, false); err != nil {
\t\tt.Fatal(err)
\t}

\tstore := newTestCuratedStore(t, root, 100_000, 100_000)
''',
    '''\tif _, err := seed.ApplyBatch(CuratedTargetCurrentUser, legacyCaller, []CuratedMutation{{
\t\tAction: CuratedActionPin, ID: seedResult.Applied[0].ID,
\t}}, false); err != nil {
\t\tt.Fatal(err)
\t}
\tlegacyEntries, err := seed.List(CuratedTargetCurrentUser, legacyCaller)
\tif err != nil || len(legacyEntries) != 1 {
\t\tt.Fatalf("legacy entries=%#v err=%v", legacyEntries, err)
\t}
\tlegacyEntry := legacyEntries[0]

\tstore := newTestCuratedStore(t, root, 100_000, 100_000)
''',
)
replace_once(
    "pkg/memory/curated_migration_hardening_test.go",
    '''\tif entry.Provenance.Source != "user_request" || entry.Provenance.MessageRef != "old-message" ||
\t\t!entry.Provenance.RecordedAt.Equal(historical) || !entry.CreatedAt.Equal(historical) ||
\t\t!entry.UpdatedAt.Equal(historical) || entry.LastConfirmedAt == nil || !entry.LastConfirmedAt.Equal(historical) {
\t\tt.Fatalf("historical metadata not preserved: %#v", entry)
\t}
''',
    '''\tif entry.Provenance != legacyEntry.Provenance || !entry.CreatedAt.Equal(legacyEntry.CreatedAt) ||
\t\t!entry.UpdatedAt.Equal(legacyEntry.UpdatedAt) || entry.LastConfirmedAt == nil ||
\t\tlegacyEntry.LastConfirmedAt == nil || !entry.LastConfirmedAt.Equal(*legacyEntry.LastConfirmedAt) ||
\t\tentry.EvidenceCount != legacyEntry.EvidenceCount || entry.ObservationCount != legacyEntry.ObservationCount ||
\t\tentry.Pinned != legacyEntry.Pinned {
\t\tt.Fatalf("historical metadata not preserved: legacy=%#v migrated=%#v", legacyEntry, entry)
\t}
''',
)
