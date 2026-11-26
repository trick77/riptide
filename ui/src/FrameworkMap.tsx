// Which known metric sets Riptide's numbers belong to, and what none of
// them can show without a survey or an incident source. Static: the
// mapping changes with the frameworks, not with the data. Checked against
// dora.dev (January 2026), the DORA report 2025, DX Core 4, SPACE (ACM
// Queue 2021), DevEx (ACM Queue 2023) and Kersten's Flow Framework.

import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { Catalog } from "./i18n/en";
import { SectionHead } from "./ui";

type CellKey = keyof Catalog["frameworks"]["cell"];
type RowKey = keyof Catalog["frameworks"]["row"];
type Cell = CellKey | [kind: "gap" | "not", text: CellKey] | null;

const COLS = ["DORA", "DX Core 4", "SPACE", "DevEx", "Flow Framework"];

// Rows whose metric Riptide does not have: muted, and their cells name
// what is missing.
const MISSING: RowKey[] = ["missingCfr", "missingRecovery", "missingSurvey"];

const ROWS: [RowKey, Cell[]][] = [
  ["freq", ["dfThroughput", "speedSecondary", null, null, "velocityApprox"]],
  ["lead", ["cltThroughput", "speedSecondary", "effWait", "feedback", null]],
  [
    "stages",
    ["cltBreakdown", null, "effHandoffs", "feedback", "flowEffApprox"],
  ],
  ["dsucc", [["not", "dsuccNot"], null, null, null, null]],
  ["review", [null, null, "effWait", "feedbackReview", "flowTimeApprox"]],
  ["touch", [null, null, "collaboration", null, null]],
  ["merged", [null, ["not", "diffsTeam"], "activity", null, "flowVelocity"]],
  ["batch", ["smallBatches", null, null, null, null]],
  ["build", [null, "effectiveness", "effWait", "feedbackBuild", null]],
  ["mix", [null, "impact", null, null, "distribution"]],
  ["costs", [null, null, null, null, null]],
  [
    "missingCfr",
    [["gap", "instability"], ["gap", "qualityCore"], null, null, null],
  ],
  [
    "missingRecovery",
    [["gap", "throughput"], ["gap", "qualitySecondary"], null, null, null],
  ],
  [
    "missingSurvey",
    [
      null,
      ["gap", "dxiSurvey"],
      ["gap", "satisfactionSurvey"],
      ["gap", "surveyOnly"],
      null,
    ],
  ],
];

function Td({ cell, t }: { cell: Cell; t: TFunction }) {
  if (cell == null)
    return (
      <td className="border-b border-border-soft py-2 pr-3 align-top text-faint">
        –
      </td>
    );
  const [kind, key] = typeof cell === "string" ? ["ok", cell] : cell;
  return (
    <td
      className={`border-b border-border-soft py-2 pr-3 align-top ${
        kind === "ok"
          ? "text-ink-dim"
          : kind === "not"
            ? "text-muted"
            : "text-ochre"
      }`}
    >
      {t(`frameworks.cell.${key}`)}
    </td>
  );
}

// prod is the production stage as the page shows it.
export function FrameworkMap({ prod }: { prod: string }) {
  const { t } = useTranslation();
  return (
    <section id="frameworks" aria-labelledby="h-frameworks" className="mt-10">
      <SectionHead
        id="h-frameworks"
        eyebrow={t("frameworks.eyebrow")}
        title={t("frameworks.title")}
        lead={t("frameworks.lead")}
      />
      <div className="overflow-x-auto rounded-ui-lg border border-border bg-panel px-5 pt-4 pb-3">
        <table className="w-full min-w-[880px] border-separate border-spacing-0 text-[12.5px]">
          <thead>
            <tr>
              {["Riptide", ...COLS].map((c) => (
                <th
                  key={c}
                  className="border-b border-border pr-3 pb-2 text-left align-bottom text-[11px] font-medium tracking-[.1em] text-muted uppercase"
                >
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {ROWS.map(([row, cells]) => (
              <tr key={row}>
                <th
                  scope="row"
                  className={`border-b border-border-soft py-2 pr-3 text-left align-top text-[13px] font-normal ${
                    MISSING.includes(row) ? "text-muted" : "text-ink"
                  }`}
                >
                  {t(`frameworks.row.${row}`, { prod })}
                </th>
                {cells.map((c, i) => (
                  <Td key={i} cell={c} t={t} />
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
