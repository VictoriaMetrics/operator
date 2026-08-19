#!/usr/bin/env python3
import sys
import unittest
from datetime import datetime
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import merge_changelog as mc


def changelog(*, front_matter="---\ntitle: x\n---\n", tip="tip body", versions):
    return mc.ChangelogFile(front_matter, tip, list(versions))


def version(heading, body):
    return mc.Version(heading, body)


class ParseReleaseDateTest(unittest.TestCase):
    def test_abbreviated_month(self):
        body = "**Release date:** 12 Sep 2025\n\n* stuff"
        self.assertEqual(mc.parse_release_date(body), datetime(2025, 9, 12))

    def test_full_month_name(self):
        body = "**Release date:** 04 August 2026\n\n* stuff"
        self.assertEqual(mc.parse_release_date(body), datetime(2026, 8, 4))

    def test_missing_date(self):
        self.assertIsNone(mc.parse_release_date("* no date here"))

    def test_unparseable_date(self):
        self.assertIsNone(mc.parse_release_date("**Release date:** not-a-date"))


class MergeDedupeTest(unittest.TestCase):
    def test_shared_version_kept_once_source_body_wins(self):
        target = changelog(versions=[
            version("## [v1.1.0](url)", "**Release date:** 02 Jan 2026\n\n* target body (stale, should be ignored)"),
        ])
        source = changelog(tip="source tip", versions=[
            version("## [v1.1.0](url)", "**Release date:** 02 Jan 2026\n\n* source body"),
        ])

        merged = mc.merge(target, source, source_is_master=False)

        self.assertEqual(len(merged.versions), 1)
        self.assertIn("source body", merged.versions[0].body)
        self.assertNotIn("stale", merged.versions[0].body)

    def test_source_only_version_is_added(self):
        target = changelog(versions=[
            version("## [v1.1.0](url)", "**Release date:** 02 Jan 2026\n\n* v1.1.0"),
        ])
        source = changelog(versions=[
            version("## [v1.1.0](url)", "**Release date:** 02 Jan 2026\n\n* v1.1.0"),
            version("## [v1.2.0](url)", "**Release date:** 03 Feb 2026\n\n* v1.2.0"),
        ])

        merged = mc.merge(target, source, source_is_master=False)

        headings = [v.heading for v in merged.versions]
        self.assertEqual(headings, ["## [v1.2.0](url)", "## [v1.1.0](url)"])

    def test_repeated_merge_is_idempotent(self):
        target = changelog(versions=[
            version("## [v1.1.0](url)", "**Release date:** 02 Jan 2026\n\n* v1.1.0"),
        ])
        source = changelog(versions=[
            version("## [v1.2.0](url)", "**Release date:** 03 Feb 2026\n\n* v1.2.0"),
        ])

        once = mc.merge(target, source, source_is_master=False)
        twice = mc.merge(once, source, source_is_master=False)

        self.assertEqual(len(twice.versions), 2)
        self.assertEqual(
            [v.heading for v in once.versions],
            [v.heading for v in twice.versions],
        )


class MergeSortTest(unittest.TestCase):
    def test_sorted_descending_by_date_mixing_full_and_abbreviated_months(self):
        target = changelog(versions=[
            version("## [v1.0.0](url)", "**Release date:** 12 Sep 2025\n\n* old"),
        ])
        source = changelog(versions=[
            version("## [v1.2.0](url)", "**Release date:** 04 August 2026\n\n* newest"),
            version("## [v1.1.0](url)", "**Release date:** 02 Jan 2026\n\n* middle"),
        ])

        merged = mc.merge(target, source, source_is_master=False)

        self.assertEqual(
            [v.heading for v in merged.versions],
            ["## [v1.2.0](url)", "## [v1.1.0](url)", "## [v1.0.0](url)"],
        )

    def test_unparseable_dates_sort_last_preserving_relative_order(self):
        target = changelog(versions=[
            version("## [vA](url)", "no date here first"),
            version("## [vB](url)", "**Release date:** 02 Jan 2026\n\n* dated"),
            version("## [vC](url)", "no date here second"),
        ])
        source = changelog(versions=[])

        merged = mc.merge(target, source, source_is_master=False)

        self.assertEqual(
            [v.heading for v in merged.versions],
            ["## [vB](url)", "## [vA](url)", "## [vC](url)"],
        )


class MergeTipTest(unittest.TestCase):
    def test_master_source_replaces_tip(self):
        target = changelog(tip="old tip", versions=[])
        source = changelog(tip="new tip from master", versions=[])

        merged = mc.merge(target, source, source_is_master=True)

        self.assertEqual(merged.tip, "new tip from master")

    def test_non_master_source_tip_is_ignored(self):
        target = changelog(tip="vmdocs tip", versions=[])
        source = changelog(tip="release branch's own unreleased tip", versions=[])

        merged = mc.merge(target, source, source_is_master=False)

        self.assertEqual(merged.tip, "vmdocs tip")

    def test_only_masters_tip_survives_regardless_of_merge_order(self):
        vmdocs = changelog(tip="previously published master tip", versions=[])
        release_branch = changelog(tip="release branch's own unreleased tip", versions=[])
        master = changelog(tip="master's current unreleased tip", versions=[])

        after_release = mc.merge(vmdocs, release_branch, source_is_master=False)
        self.assertEqual(after_release.tip, "previously published master tip")

        after_master = mc.merge(after_release, master, source_is_master=True)
        self.assertEqual(after_master.tip, "master's current unreleased tip")

        final = mc.merge(after_master, release_branch, source_is_master=False)
        self.assertEqual(final.tip, "master's current unreleased tip")


class ParseRenderRoundTripTest(unittest.TestCase):
    FRONT_MATTER = "---\ntitle: CHANGELOG\n---\n"

    def build(self, tip_body, *version_blocks):
        parts = [self.FRONT_MATTER, "\n## tip\n\n", tip_body]
        for heading, body in version_blocks:
            parts.append(f"\n\n{heading}\n{body}")
        parts.append("\n")
        return "".join(parts)

    def test_end_to_end_merge_of_master_into_vmdocs(self):
        target_data = self.build(
            "old unreleased notes",
            ("## [v1.1.0](url)", "**Release date:** 02 Jan 2026\n\n* v1.1.0 note"),
            ("## [v1.0.0](url)", "**Release date:** 12 Sep 2025\n\n* v1.0.0 note"),
        )
        source_data = self.build(
            "new unreleased notes",
            ("## [v1.2.0](url)", "**Release date:** 04 August 2026\n\n* v1.2.0 note"),
            ("## [v1.1.0](url)", "**Release date:** 02 Jan 2026\n\n* v1.1.0 note (from operator repo)"),
        )

        target = mc.parse(target_data)
        source = mc.parse(source_data)
        merged = mc.merge(target, source, source_is_master=True)

        self.assertEqual(merged.tip, "new unreleased notes")
        self.assertEqual(
            [v.heading for v in merged.versions],
            ["## [v1.2.0](url)", "## [v1.1.0](url)", "## [v1.0.0](url)"],
        )
        v1_1_0 = next(v for v in merged.versions if v.heading == "## [v1.1.0](url)")
        self.assertIn("from operator repo", v1_1_0.body)

        rendered = merged.render()
        reparsed = mc.parse(rendered)
        self.assertEqual(reparsed.tip, merged.tip)
        self.assertEqual(
            [v.heading for v in reparsed.versions],
            [v.heading for v in merged.versions],
        )

    def test_lts_release_branch_preserves_vmdocs_tip(self):
        target_data = self.build(
            "vmdocs unreleased notes",
            ("## [v0.68.7](url)", "**Release date:** 12 Sep 2025\n\n* note"),
        )
        source_data = self.build(
            "release branch's own not-yet-tagged notes",
            ("## [v0.68.8](url)", "**Release date:** 20 Sep 2025\n\n* note"),
        )

        target = mc.parse(target_data)
        source = mc.parse(source_data)
        merged = mc.merge(target, source, source_is_master=False)

        self.assertEqual(merged.tip, "vmdocs unreleased notes")
        self.assertEqual(
            [v.heading for v in merged.versions],
            ["## [v0.68.8](url)", "## [v0.68.7](url)"],
        )


if __name__ == "__main__":
    unittest.main()
