// Which known metric sets Riptide's numbers belong to, and what none of
// them can show without a survey or an incident source. Static: the
// mapping changes with the frameworks, not with the data. Checked against
// dora.dev (January 2026), the DORA report 2025, DX Core 4, SPACE (ACM
// Queue 2021), DevEx (ACM Queue 2023) and Kersten's Flow Framework.

import { SectionHead } from "./ui";

type Cell = string | [kind: "gap" | "not", text: string] | null;

const COLS = ["DORA", "DX Core 4", "SPACE", "DevEx", "Flow Framework"];

const ROWS: [string, Cell[]][] = [
  [
    "Deploy-Frequenz",
    [
      "Deployment frequency · Throughput",
      "Speed, sekundär",
      null,
      null,
      "Flow velocity, angenähert",
    ],
  ],
  [
    "Lead Time bis prod",
    [
      "Change lead time · Throughput",
      "Speed, sekundär",
      "Efficiency & flow (Wartezeit)",
      "Feedback loops",
      null,
    ],
  ],
  [
    "Fluss nach Etappen",
    [
      "Aufschlüsselung der Change lead time",
      null,
      "Efficiency & flow (Übergaben, Wartezeit)",
      "Feedback loops",
      "Flow efficiency, angenähert",
    ],
  ],
  [
    "Deploy-Erfolg",
    [
      ["not", "keine DORA-Metrik: zählt Sync-Fehler, nicht Change fail rate"],
      null,
      null,
      null,
      null,
    ],
  ],
  [
    "Review-Zeit, innert 24 h gemergt",
    [
      null,
      [
        "not",
        "Speed misst «Diffs pro Engineer»: misst Riptide bewusst nicht, das vergleicht Personen",
      ],
      "Efficiency & flow (Wartezeit)",
      "Feedback loops (Code-Review)",
      "Flow time, angenähert",
    ],
  ],
  [
    "Approval ohne Kommentar",
    [null, null, "Communication & collaboration (Review-Qualität)", null, null],
  ],
  [
    "Arbeitsmix, ohne Jira-Key",
    [
      null,
      "Impact: Näherung für «Zeit für Neues», zählt PRs statt Zeit",
      null,
      null,
      "Flow distribution: Feature und Bugfix, ohne Debt und Risk",
    ],
  ],
  [
    "Build: Erfolg, Dauer, rote Läufe",
    [
      null,
      "Effectiveness: ein Treiber, nicht der DXI selbst",
      "Efficiency & flow (Wartezeit)",
      "Feedback loops (Build)",
      null,
    ],
  ],
  ["Kosten der KI-Reviews", [null, null, null, null, null]],
  [
    "fehlt: Change fail rate, Deployment rework rate",
    [["gap", "Instability"], ["gap", "Quality, Kernmetrik"], null, null, null],
  ],
  [
    "fehlt: Failed deployment recovery time",
    [["gap", "Throughput"], ["gap", "Quality, sekundär"], null, null, null],
  ],
  [
    "fehlt: Zufriedenheit, Cognitive load, Flow state",
    [
      null,
      ["gap", "Effectiveness: DXI, nur per Umfrage"],
      ["gap", "Satisfaction & well-being, nur per Umfrage"],
      ["gap", "nur per Umfrage"],
      null,
    ],
  ],
];

function Td({ cell }: { cell: Cell }) {
  if (cell == null)
    return (
      <td className="border-b border-border-soft py-2 pr-3 align-top text-faint">
        –
      </td>
    );
  const [kind, text] = typeof cell === "string" ? ["ok", cell] : cell;
  return (
    <td
      className={`border-b border-border-soft py-2 pr-3 align-top ${
        kind === "ok" ? "text-ink-dim" : "text-ochre"
      }`}
    >
      {text}
    </td>
  );
}

export function FrameworkMap() {
  return (
    <section id="frameworks" aria-labelledby="h-frameworks" className="mt-10">
      <SectionHead
        id="h-frameworks"
        eyebrow="Einordnung"
        title="Welche Frameworks Riptide abdeckt"
        lead="Riptide misst aus den Daten der Tools, was ohne Umfrage messbar ist: von den fünf DORA-Metriken zwei, DX Core 4 teilweise. SPACE und DevEx brauchen zusätzlich Befragungen."
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
            {ROWS.map(([name, cells]) => (
              <tr key={name}>
                <th
                  scope="row"
                  className={`border-b border-border-soft py-2 pr-3 text-left align-top text-[13px] font-normal ${
                    name.startsWith("fehlt") ? "text-muted" : "text-ink"
                  }`}
                >
                  {name}
                </th>
                {cells.map((c, i) => (
                  <Td key={i} cell={c} />
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="mt-2.5 mb-0 max-w-[110ch] text-xs text-muted">
        Change fail rate, Deployment rework rate und Failed deployment recovery
        time brauchen eine Incident-Quelle, die Riptide nicht hat. DORA führt
        seit dem Report 2025 keine Leistungsstufen mehr und nennt den Vergleich
        desselben Dienstes über die Zeit den aussagekräftigsten. Riptide
        vergleicht darum jedes Team nur mit sich selbst, nie mit Umfragewerten
        oder anderen Teams. DORA und DX Core 4 betonen beide: Metriken gelten
        für Teams und Anwendungen, nie für Personen.
      </p>
    </section>
  );
}
