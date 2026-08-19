from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one match, found {count}: {old!r}")
    p.write_text(text.replace(old, new, 1))


# The capability helper below became redundant once ordinary Telegram media
# delivery learned to classify animation/sticker/video-note failures directly.
p = Path("pkg/channels/telegram/native_single_media_delivery.go")
text = p.read_text()
start = text.index("func nativeSingleMediaCapability(")
end = text.index("func telegramMediaCapability(", start)
p.write_text(text[:start] + text[end:])

# Avoid govet shadow warnings in the migration metadata regression while
# retaining explicit error checks for every storage operation.
replace_once(
    "pkg/memory/curated_migration_hardening_test.go",
    "\tseed, err := NewCuratedStore(root, CuratedStoreOptions{",
    "\tseed, seedErr := NewCuratedStore(root, CuratedStoreOptions{",
)
replace_once(
    "pkg/memory/curated_migration_hardening_test.go",
    "\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tlegacyKey :=",
    "\tif seedErr != nil {\n\t\tt.Fatal(seedErr)\n\t}\n\tlegacyKey :=",
)
replace_once(
    "pkg/memory/curated_migration_hardening_test.go",
    "\tseedResult, err := seed.ApplyBatch(CuratedTargetCurrentUser, legacyCaller, []CuratedMutation{{",
    "\tseedResult, seedApplyErr := seed.ApplyBatch(CuratedTargetCurrentUser, legacyCaller, []CuratedMutation{{",
)
replace_once(
    "pkg/memory/curated_migration_hardening_test.go",
    "\tif err != nil || len(seedResult.Applied) != 1 {\n\t\tt.Fatalf(\"seed result=%#v err=%v\", seedResult, err)\n\t}",
    "\tif seedApplyErr != nil || len(seedResult.Applied) != 1 {\n\t\tt.Fatalf(\"seed result=%#v err=%v\", seedResult, seedApplyErr)\n\t}",
)
replace_once(
    "pkg/memory/curated_migration_hardening_test.go",
    "\tif _, err := seed.ApplyBatch(CuratedTargetCurrentUser, legacyCaller, []CuratedMutation{{\n\t\tAction: CuratedActionPin, ID: seedResult.Applied[0].ID,\n\t}}, false); err != nil {\n\t\tt.Fatal(err)\n\t}",
    "\tif _, pinErr := seed.ApplyBatch(CuratedTargetCurrentUser, legacyCaller, []CuratedMutation{{\n\t\tAction: CuratedActionPin, ID: seedResult.Applied[0].ID,\n\t}}, false); pinErr != nil {\n\t\tt.Fatal(pinErr)\n\t}",
)
replace_once(
    "pkg/memory/curated_migration_hardening_test.go",
    "\tlegacyEntries, err := seed.List(CuratedTargetCurrentUser, legacyCaller)\n\tif err != nil || len(legacyEntries) != 1 {\n\t\tt.Fatalf(\"legacy entries=%#v err=%v\", legacyEntries, err)\n\t}",
    "\tlegacyEntries, legacyListErr := seed.List(CuratedTargetCurrentUser, legacyCaller)\n\tif legacyListErr != nil || len(legacyEntries) != 1 {\n\t\tt.Fatalf(\"legacy entries=%#v err=%v\", legacyEntries, legacyListErr)\n\t}",
)
replace_once(
    "pkg/memory/curated_migration_hardening_test.go",
    "\tif _, err := store.MigrateLegacyUserStoreToPersonScope([]string{legacyKey}, \"person:owner\"); err != nil {\n\t\tt.Fatal(err)\n\t}",
    "\tif _, migrateErr := store.MigrateLegacyUserStoreToPersonScope([]string{legacyKey}, \"person:owner\"); migrateErr != nil {\n\t\tt.Fatal(migrateErr)\n\t}",
)
replace_once(
    "pkg/memory/curated_migration_hardening_test.go",
    "\tentries, err := store.List(CuratedTargetCurrentUser, CallerScope{AgentID: \"migration\", UserKey: \"person:owner\"})\n\tif err != nil || len(entries) != 1 {\n\t\tt.Fatalf(\"entries=%#v err=%v\", entries, err)\n\t}",
    "\tentries, personListErr := store.List(CuratedTargetCurrentUser, CallerScope{AgentID: \"migration\", UserKey: \"person:owner\"})\n\tif personListErr != nil || len(entries) != 1 {\n\t\tt.Fatalf(\"entries=%#v err=%v\", entries, personListErr)\n\t}",
)

# QZ-02: don't carry the legacy singular upstream field spelling in new tests.
replace_once(
    "pkg/tools/poll_test.go",
    't.Fatal("expected error for out-of-range correct_option_id")',
    't.Fatal("expected error for out-of-range correct option ID")',
)
