// Bits more than one view draws the same: the heading separator, what an
// open PR waits on, the table classes.

import type { PRWaiting } from "./api";

// A middot between parts of a heading or title, an en space either side:
// plain spaces read cramped (user). A literal U+2002 trips eslint.
const EN = String.fromCharCode(0x2002);
export const SEP = `${EN}·${EN}`;

// What an open PR waits on, in the order of the wait chain: the author's,
// then the reviewers', then the merge; the draft last.
export const WAITS: readonly PRWaiting[] = [
  "first_review",
  "review",
  "changes",
  "merge",
  "draft",
];

// A wait's colour wherever it shows: the bar fades from the series' token
// at its top to 55 % at its foot, the swatch is the token. A draft is grey,
// its author's, not the team's. Full class names, so Tailwind finds them.
export const WAIT_FILL: Record<PRWaiting, { bar: string; swatch: string }> = {
  first_review: { bar: "from-s1 to-s1/55", swatch: "bg-s1" },
  review: { bar: "from-s3 to-s3/55", swatch: "bg-s3" },
  changes: { bar: "from-s4 to-s4/55", swatch: "bg-s4" },
  merge: { bar: "from-s2 to-s2/55", swatch: "bg-s2" },
  draft: { bar: "from-other to-other/55", swatch: "bg-other" },
};

export const TABLE_HEAD =
  "border-b border-border pb-2 text-[11px] font-medium tracking-[.12em] text-muted uppercase";
export const TABLE_CELL =
  "border-b border-border-soft py-2 text-[13.5px] tabular-nums";
