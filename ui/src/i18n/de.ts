import type { Catalog } from "./en";
import terms from "./terms.json";

// German UI catalog, shown for every de-* browser language unless the
// header's switch says otherwise. Swiss orthography: double s, never
// eszett. No gendered forms: generic masculine ("der Autor", "der
// Reviewer"). One word per thing, as docs/metrics.md "Words on the page"
// lists them, the owner's choice (2026-11-26) among them: the default
// branch, a Bitbucket project, a library and a rollout keep their usual
// names; Freigabe for an approval, PR-Grösse for the batch size, Abschnitt
// for a piece of the way, typisch for the median.
export const de = {
  lang: {
    code: "DE",
    switch: "Sprache: Deutsch. Auf Englisch wechseln",
  },
  nav: {
    label: "Bereiche",
    fluss: "Fluss",
    build: "Build",
    deploys: "Deploys",
    arbeitsmix: "Arbeitsmix",
    kosten: "Kosten",
    daten: "Daten",
    frameworks: "Frameworks",
    einschaetzung: "Einschätzung",
    ausliefern: "Rollout",
    review: "Review",
    more: "Weitere",
    view: "{{product}} · {{span}}",
  },
  trends: {
    title: "Gemergte PRs und PR-Grösse pro Woche",
    sub: "Nur PRs von Menschen, ohne Bots. {{span}}: Vorperiode, Zeitraum und die laufende Woche.",
    merged: "Gemergte PRs pro Woche",
    mergedUnit: "pro Woche im Zeitraum",
    batch: "PR-Grösse pro Woche, typisch",
    batchUnit: "geänderte Zeilen pro PR, typisch",
    releases: "Releases nach {{prod}} pro Woche",
    before: "{{unit}}, vorher {{v}}",
    previous: "Vorperiode",
    period: "Zeitraum",
    running: "laufende Woche, bis jetzt",
    legendPrevious: "Vorperiode, {{span}}",
    legendWeek: "pro Woche",
    legendRunning: "laufende Woche, noch nicht vorbei",
    gap: {
      no_data: "keine Daten: Die Quelle lieferte damals noch nichts",
      silent: "keine Daten: Die Quelle liefert seither nichts mehr",
      lost: "Daten verloren: Bitbucket lieferte keine Daten",
      none: "kein PR mit bekannter Grösse",
    },
  },
  repos: {
    head: "Pro Repo",
    headText:
      "Jedes Repo wird für sich beurteilt, nie im Schnitt mit anderen. Ein Klick auf eine Zeile zeigt die PRs des Repos unten in den Daten.",
    title: "Repos, das Auffällige zuerst",
    sub: "Die Balken zeigen die Releases pro Woche, {{span}}; die Vorperiode ist hinterlegt.",
    col: {
      repo: "Repo",
      stand: "Auffällig",
      releases: "Wie oft nach {{prod}}",
      toProd: "Vom Merge bis {{prod}}, typisch",
      merged: "Gemergte PRs pro Woche",
      batch: "PR-Grösse, typisch",
    },
    group: {
      none: "Projekt ohne Release seit über {{breach}} Tagen",
      never: "Noch kein {{prod}}-Release",
      rare: "Letztes Release des Projekts vor {{from}}–{{breach}} Tagen",
      unjudged: "Nicht beurteilbar",
      regular: "Projekt mit Release in den letzten {{hint}} Tagen",
      library: "Libraries",
      idle: "Ohne Aktivität",
    },
    groupSub: {
      none: "obwohl daran gearbeitet wurde",
      never: "Das Projekt kam laut den Daten noch nie nach {{prod}}",
      unjudged: "Die Daten zeigen nicht, ob und wann das Repo ausgerollt wird",
      library: "werden nie selbst ausgerollt, darum fehlt hier kein Release",
      idle: "kein gemergter PR seit dem letzten Release des Projekts und keiner in {{breach}} Tagen",
    },
    flag: {
      no_release_one: "seit {{lower}}{{count}} Tag kein Release",
      no_release_other: "seit {{lower}}{{count}} Tagen kein Release",
      release_due_one: "letztes Release vor {{lower}}{{count}} Tag",
      release_due_other: "letztes Release vor {{lower}}{{count}} Tagen",
      not_on_prod_one: "ältester PR seit {{count}} Tag nicht auf {{prod}}",
      not_on_prod_other: "ältester PR seit {{count}} Tagen nicht auf {{prod}}",
      slow_to_prod: "typisch {{days}} Tage bis {{prod}}",
      first_review_wait: "typisch {{days}} Arbeitstage bis zum ersten Review",
      lowerBound: "mindestens ",
    },
    reason: {
      not_reporting: "Argo CD liefert keine Daten",
      argocd_silent: "Argo CD liefert keine Daten mehr",
      bitbucket_silent: "Bitbucket liefert keine Daten mehr",
      no_prod_stage: "Argo CD liefert keine Daten zur Stage {{prod}}",
      infra: "nur Infrastruktur-Apps",
      untraced: "keine App bekannt, die es ausrollt",
    },
    pill: {
      library: "Library",
      idle: "ohne Aktivität",
    },
    rel: {
      none: "keines im Zeitraum",
      perWeek: "{{n}} pro Woche",
      inWeeks_one: "{{count}} Release in {{weeks}}",
      inWeeks_other: "{{count}} Releases in {{weeks}}",
      weeks_one: "{{count}} Woche",
      weeks_other: "{{count}} Wochen",
      never: "noch kein {{prod}}-Release des Projekts",
      last_one: "Projekt: letztes vor {{count}} Tag",
      last_other: "Projekt: letztes vor {{count}} Tagen",
      lastLower_one: "Projekt: letztes vor mindestens {{count}} Tag",
      lastLower_other: "Projekt: letztes vor mindestens {{count}} Tagen",
    },
    lying: "kein PR auf {{prod}}",
    thinCell_one: "zu wenige ({{count}} PR)",
    thinCell_other: "zu wenige ({{count}} PRs)",
    notYet_one: "{{count}} PR noch nicht auf {{prod}}",
    notYet_other: "{{count}} PRs noch nicht auf {{prod}}",
    rowTitle: "PRs von {{repo}} in den Daten zeigen",
    mergedAria: "Gemergte PRs pro Woche in {{repo}}",
    restOf_one: "und {{count}} weiteres Repo dieser Gruppe:",
    restOf_other: "und {{count}} weitere Repos dieser Gruppe:",
    andMore: "und {{n}} weitere",
    all: "alle {{n}} zeigen",
    less: "nur die ersten {{n}} zeigen",
    countAll_one: "{{count}} Repo.",
    countAll_other: "{{count}} Repos.",
    countOpen: "{{n}} von {{total}} Repos gezeigt.",
    order:
      "Reihenfolge: zuerst die Repos, deren Projekt seit über {{breach}} Tagen kein Release hat, dann die, deren Projekt noch nie nach {{prod}} kam, die mit einem Release des Projekts vor {{from}}–{{breach}} Tagen, die nicht beurteilbaren und die übrigen (wer die Abmachung zur Zeit bis {{prod}} verletzt, zuerst); am Schluss Libraries und Repos ohne Aktivität.",
    unit: {
      perWeek: "{{n}} pro Woche",
      workingDays: "{{n}} Arbeitstage",
      lines: "{{n}} Zeilen",
    },
    stripAria:
      "{{label}}: ein Punkt pro Repo, {{n}} Repos. Pfeiltasten wechseln das Repo, Enter zeigt seine PRs.",
    verdict: {
      breach: "Abmachung verletzt",
      hint: "Hinweis",
      within: "innerhalb der Abmachung",
      neutral: "",
    },
    typicalMark: "typisch",
    same_one: "{{count}} Repo: {{value}}",
    same_other: "Alle {{count}} Repos: {{value}}",
    apartLine_one: "{{count}} Repo hat im Zeitraum nichts ausgerollt.",
    apartLine_other: "{{count}} Repos haben im Zeitraum nichts ausgerollt.",
    typical_one: "Das typische Repo: {{value}}, aus {{count}} Repo",
    typical_other: "Das typische Repo: {{value}}, aus {{count}} Repos",
    apart: {
      releases: "kein Release im Zeitraum",
      to_prod: "kein PR ausgerollt",
    },
    n: {
      releases_one: "{{count}} Release im Zeitraum",
      releases_other: "{{count}} Releases im Zeitraum",
      to_prod_one: "{{count}} PR",
      to_prod_other: "{{count}} PRs",
      merged_one: "{{count}} gemergter PR",
      merged_other: "{{count}} gemergte PRs",
      batch_one: "{{count}} PR mit bekannter Grösse",
      batch_other: "{{count}} PRs mit bekannter Grösse",
      pickup_one: "{{count}} PR",
      pickup_other: "{{count}} PRs",
      build_duration_one: "{{count}} erfolgreicher Build",
      build_duration_other: "{{count}} erfolgreiche Builds",
      build_success_one: "{{count}} Build",
      build_success_other: "{{count}} Builds",
    },
    cases: {
      releases: "Releases",
      to_prod: "PRs",
      merged: "gemergten PRs",
      batch: "PRs mit bekannter Grösse",
      pickup: "PRs",
      build_duration: "Builds",
      build_success: "Builds",
    },
    note: {
      dominant: "{{repo}} allein bringt {{share}} aller {{cases}} des Teams.",
      far_one: "{{count}} von {{of}} Repos liegt nahe beim typischen Repo",
      far_other: "{{count}} von {{of}} Repos liegen nahe beim typischen Repo",
      above_one: "{{count}} weit darüber",
      above_other: "{{count}} weit darüber",
      below_one: "{{count}} weit darunter",
      below_other: "{{count}} weit darunter",
      pooled:
        "Die Zahl des Teams nimmt alle {{cases}} in einen Topf: Repos mit vielen davon bestimmen sie. Das typische Repo liegt bei {{value}}.",
      pooledAdded:
        "Die Zahl des Teams zählt alle Repos zusammen; das typische Repo allein kommt auf {{value}}.",
    },
    thin_one:
      "{{count}} Repo mit zu wenigen {{cases}} für einen typischen Wert",
    thin_other:
      "{{count}} Repos mit zu wenigen {{cases}} für einen typischen Wert",
    strip: {
      releases: "Pro Repo: Releases nach {{prod}} pro Woche",
      toProd: "Pro Repo: vom Merge bis {{prod}}, typisch",
      merged: "Pro Repo: gemergte PRs pro Woche",
      batch: "Pro Repo: PR-Grösse, typisch",
      pickup: "Pro Repo: Wartezeit bis zum ersten Review, typisch",
      buildSuccess: "Pro Repo: Anteil erfolgreicher Builds",
      buildDuration: "Pro Repo: Dauer eines erfolgreichen Builds, typisch",
    },
  },
  common: {
    cw: "KW {{kw}}",
    cwSpan: "KW {{from}}–{{to}}",
    cwSpanYears: "KW {{from}}/{{fromYear}}–KW {{to}}/{{toYear}}",
    hours: "{{n}} h",
    nOf: "{{n}} von {{total}}",
    nPRs_one: "{{count}} PR",
    nPRs_other: "{{count}} PRs",
    lever: "Was die Zahl bewegt",
    typical: "{{unit}}, typisch",
    slowest: "die langsamsten 10 %: über {{p90}}",
    lowSample: "wenig belastbar",
    noMergedPRs: "Im Zeitraum wurde kein PR gemerged.",
    shorterBetter: "kürzer ist besser",
    ownHistory: "Jede Zahl nur mit dem eigenen Verlauf des Teams vergleichen.",
    noDirection: "kein Ziel",
    withoutBots: "ohne Bots",
    withoutGitOps: "ohne GitOps-Repos",
    releases_one: "{{count}} Release",
    releases_other: "{{count}} Releases",
    resyncs_one: "{{count}} Deploy ohne neue Version",
    resyncs_other: "{{count}} Deploys ohne neue Version",
    firstDeploys_one: "{{count}} erstes Deploy",
    firstDeploys_other: "{{count}} erste Deploys",
    revisions_one: "{{count}} Änderung im GitOps-Repo",
    revisions_other: "{{count}} Änderungen im GitOps-Repo",
  },
  units: {
    s: "{{n}} s",
    min: "{{n}} min",
    h: "{{n}} h",
    days: "{{n}} Tage",
  },
  page: {
    team: "Team",
    print: "Drucken / PDF",
    title: "Engineering-Metriken",
    scope:
      "Team {{team}}, {{product}} · {{span}} · {{start}} bis {{end}} · verglichen mit den {{weeks}} Wochen davor, {{prevStart}} bis {{prevEnd}}",
    scopeAlone: "Team {{team}}, {{product}} · {{span}} · {{start}} bis {{end}}",
    running:
      "Laufende Woche, KW {{kw}}, bis jetzt: {{releases}}, {{merged}}, {{runs}}. Sie zählt in keiner Zahl dieser Seite mit; nur die offenen PRs zeigen den Stand von jetzt.",
    runningReleases_one: "{{count}} Release",
    runningReleases_other: "{{count}} Releases",
    runningMerged_one: "{{count}} PR gemerged",
    runningMerged_other: "{{count}} PRs gemerged",
    runningRuns_one: "{{count}} Build",
    runningRuns_other: "{{count}} Builds",
    runningView: "Ansehen →",
    oneProduct: "Projekt {{product}}",
    allProductsLower: "alle Projekte",
    dataAsOf: "Datenstand:",
    source: "{{source}} seit {{since}}, zuletzt {{at}}",
    sourceNone: "{{source}}: keine Daten",
    repoStart: "{{repo}}: seit {{date}}",
    repoNever: "{{repo}}: keine Daten",
    lost: "{{source}} lieferte vom {{from}} bis {{to}} keine Daten, obwohl Builds liefen: Ereignisse gingen verloren.",
    lostOngoing:
      "{{source}} liefert seit dem {{from}} keine Daten, obwohl Builds laufen: Ereignisse gehen verloren.",
    filter: "Filter",
    period: "Zeitraum",
    weeks: "{{n}} W",
    product: "Projekt",
    allProducts: "Alle Projekte",
    repos_one: "{{count}} Repo",
    repos_other: "{{count}} Repos",
    reposHead: "Repos",
    appsHead: "Argo-CD-Apps",
    none: "keine",
    loadFailed: "Daten konnten nicht geladen werden: {{msg}}",
    noTeam: "In der Konfiguration ist kein Team eingetragen.",
    loading: "Lädt",
  },
  compare: {
    better: "Besser",
    worse: "Schlechter",
    same: "Unverändert",
    none: "kein Vergleich",
    low_sample:
      "Kein Vergleich: zu wenig Daten im Zeitraum oder in der Vorperiode.",
    source_started:
      "Kein Vergleich: {{source}} liefert erst seit dem {{date}} Daten.",
    lost: "Kein Vergleich: Bitbucket lieferte vom {{from}} bis {{to}} keine Daten.",
    lostOngoing:
      "Kein Vergleich: Bitbucket liefert seit dem {{from}} keine Daten.",
    first_deploys:
      "Kein Vergleich: Im Zeitraum oder in der Vorperiode überwiegen erste Deploys.",
    incomplete:
      "Kein Vergleich: Die Daten im Zeitraum und in der Vorperiode sind nicht gleich vollständig.",
    no_value:
      "Kein Vergleich: Im Zeitraum oder in der Vorperiode fehlt der Wert.",
    not_reporting:
      "Kein Vergleich: {{source}} liefert für das Team noch keine Daten.",
    previous: "Vorperiode",
    now: "jetzt",
    before: "vorher {{v}}",
    undirected: "kein Urteil: Hier ist weder mehr noch weniger besser",
  },
  strip: {
    title: "Verglichen mit der Vorperiode",
    text: "Links schlechter, rechts besser. Je länger der Balken, desto grösser die Veränderung; ab Halbierung oder Verdoppelung ist er voll. Zahlen ohne Ziel fehlen hier: Bei ihnen ist weder mehr noch weniger besser.",
    aria: "Jede Zahl verglichen mit der Vorperiode",
    reason: {
      low_sample: "zu wenig Daten",
      source_started: "Quelle erst seit Kurzem",
      lost: "Daten verloren",
      first_deploys: "erste Deploys überwiegen",
      incomplete: "Daten lückenhaft",
      no_value: "kein Wert",
      not_reporting: "{{source}} liefert keine Daten",
    },
    buildSuccess: "Erfolgreiche Builds, {{kind}}",
  },
  why: {
    button: "Was heisst das?",
    means: "Was heisst das? · {{title}}",
    watch: "Genauer hinschauen, wenn",
    def: "Wie gezählt",
    basis: "Datenbasis",
    coverage: "Vollständigkeit",
  },
  frameworksLabel: "Einordnung",
  chart: {
    noData: "keine verlässlichen Daten",
  },
  overview: {
    eyebrow: "Überblick",
    title: "Einschätzung",
    intro:
      "Was die letzten {{weeks}} ganzen Wochen zeigen, und wo sich am meisten bewegt.",
    assess: {
      ask: "↻ Neu einschätzen",
      askFirst: "Jetzt einschätzen",
      running: "läuft …",
      cooldown: "Wieder möglich ab {{time}}",
      madeAt: "Einschätzung vom {{date}}",
      trigger: {
        scheduled: "automatisch",
        manual: "auf Knopfdruck",
      },
      next: "nächste: {{date}}",
      older_one: "Stand vor {{count}} Woche",
      older_other: "Stand vor {{count}} Wochen",
      runningNote:
        "Die Einschätzung für {{weeks}} läuft, meist unter einer Minute.",
      failed: "Die letzte Einschätzung ist fehlgeschlagen: {{detail}}",
      askFailed: "Einschätzung nicht gestartet: {{detail}}",
      outdated:
        "Die letzte Einschätzung beruht auf älteren Regeln und wird nicht mehr gezeigt. Hier steht, was die aktuellen Regeln sehen.",
      outdatedAsk: " «Neu einschätzen» schreibt sie neu.",
      limitsLabel: "Was diese Einschätzung einschränkt",
      limit: {
        starts: "{{source}} liefert erst seit dem {{date}} Daten.",
        silence:
          "{{source}} lieferte vom {{from}} bis {{to}} keine Daten, obwohl Builds liefen: Ereignisse gingen verloren. Darum bleiben die PR-Zahlen ohne Urteil.",
        silenceProduct:
          "{{source}} lieferte vom {{from}} bis {{to}} keine Daten, obwohl Builds liefen: Ereignisse gingen verloren. Darum bleiben die PR-Zahlen ohne Urteil, und auch Deploys und Builds: Sie finden ihr Projekt nur über Bitbucket.",
        silenceOngoing:
          "{{source}} liefert seit dem {{from}} keine Daten, obwohl Builds laufen: Ereignisse gehen verloren. Darum bleiben die PR-Zahlen ohne Urteil.",
        silenceOngoingProduct:
          "{{source}} liefert seit dem {{from}} keine Daten, obwohl Builds laufen: Ereignisse gehen verloren. Darum bleiben die PR-Zahlen ohne Urteil, und auch Deploys und Builds: Sie finden ihr Projekt nur über Bitbucket.",
        silent_end:
          "{{source}} liefert seit dem {{from}} bis zum Ende des Zeitraums keine Daten mehr. Zu diesen Zahlen sagt die Einschätzung nichts.",
      },
      reason: {
        no_baseline:
          "Noch kein voller Vergleich mit der Vorperiode: Nicht jede Zahl hat dort einen vergleichbaren Wert.",
        no_baseline_none:
          "Noch kein Vergleich möglich: Für die Vorperiode gibt es keine vergleichbaren Daten.",
        notMeasurable: {
          bitbucket:
            "Nicht messbar: Bitbucket liefert für das Team keine Daten.",
          argocd: "Nicht messbar: Argo CD liefert für das Team keine Daten.",
          noMerged:
            "Nicht messbar: In diesen Wochen wurde kein PR gemerged, der als Arbeit zählt.",
          noRelease: "In diesen Wochen ging nichts nach {{prod}}.",
          toProd_one:
            "Nicht messbar: {{count, num}} PR wurde in diesen Wochen gemerged und liess sich nicht bis {{prod}} verfolgen.",
          toProd_other:
            "Nicht messbar: {{count, num}} PRs wurden in diesen Wochen gemerged, keiner liess sich bis {{prod}} verfolgen.",
          toProdRolled_one:
            "Nicht messbar: {{count, num}} PR aus einem Repo, das ausgerollt wird, wurde in diesen Wochen gemerged und liess sich nicht bis {{prod}} verfolgen.",
          toProdRolled_other:
            "Nicht messbar: {{count, num}} PRs aus Repos, die ausgerollt werden, wurden in diesen Wochen gemerged, keiner liess sich bis {{prod}} verfolgen.",
        },
      },
      librariesOnly:
        "Dieses Projekt enthält nur Libraries; sie werden nicht ausgerollt.",
      librariesOnlyTeam:
        "Alle PRs, die in diesen Wochen gemerged wurden, liegen in Libraries; sie werden nicht ausgerollt.",
      quiet: "In diesen Wochen fällt nichts auf.",
      source: {
        bitbucket: "Bitbucket",
        pipeline: "Die CI",
        argocd: "Argo CD",
        noergler: "noergler",
      },
      focus: "Im Fokus",
      focusBy: "nach Gewicht der Befunde",
      scopeAll: "Team, alle Projekte",
      scopeProduct: "Projekt {{product}}",
      col: {
        good: "Läuft",
        bad: "Hakt",
        done: "Erledigt",
        hint: "Hinweise",
        limit: "Nicht beurteilbar",
      },
      colSub: {
        hint: "kein Befund, nur zur Kenntnis",
        limit: "dazu sagen die Daten nichts",
      },
      // A state's card. Every number comes from the backend: the units'
      // values and the team's thresholds (riptide.json), never written into
      // a text. "Abmachung" is the team's own agreement (owner, 2026-11);
      // "Gemeldet ab" a threshold that is no agreement.
      state: {
        // A span in a state's words: {{n}} is the backend's value.
        span: {
          minutes_one: "{{n}} Minute",
          minutes_other: "{{n}} Minuten",
          wd_one: "{{n}} Arbeitstag",
          wd_other: "{{n}} Arbeitstage",
          wdDat_one: "{{n}} Arbeitstag",
          wdDat_other: "{{n}} Arbeitstagen",
          days_one: "{{n}} Tag",
          days_other: "{{n}} Tage",
          daysDat_one: "{{n}} Tag",
          daysDat_other: "{{n}} Tagen",
        },
        unitWord: {
          project_one: "Projekt",
          project_other: "Projekte",
          repo_one: "Repo",
          repo_other: "Repos",
          app_one: "App",
          app_other: "Apps",
          branch_one: "Branch",
          branch_other: "Branches",
          botBranch_one: "Bot-Branch",
          botBranch_other: "Bot-Branches",
        },
        of: {
          project_one: "{{k}} von {{n}} Projekt",
          project_other: "{{k}} von {{n}} Projekten",
          repo_one: "{{k}} von {{n}} Repo",
          repo_other: "{{k}} von {{n}} Repos",
          app_one: "{{k}} von {{n}} App",
          app_other: "{{k}} von {{n}} Apps",
          branch_one: "{{k}} von {{n}} Branch",
          branch_other: "{{k}} von {{n}} Branches",
          botBranch_one: "{{k}} von {{n}} Bot-Branch",
          botBranch_other: "{{k}} von {{n}} Bot-Branches",
        },
        more_one: "{{count}} mehr anzeigen",
        more_other: "{{count}} mehr anzeigen",
        less: "weniger zeigen",
        since: "seit dem {{date}}",
        sinceAtLeast: "spätestens seit dem {{date}}",
        asOf: "Stand {{date}}",
        unjudged_one: "{{n}} {{unit}} mit zu wenig Daten",
        unjudged_other: "{{n}} {{unit}} mit zu wenig Daten",
        held: "(knapp unter der Grenze, darum noch genannt)",
        twoSprints: "(seit zwei Sprints oder länger)",
        folded:
          "{{label}}: {{value}}, deutlich schlechter als in der Vorperiode",
        unitFacts: "{{name}} in den heutigen Daten ansehen",
        facts: "Heute «{{term}}» in den Daten ansehen →",
        no_release: {
          title: "Lange kein Release nach {{prod}}",
          unit_one:
            "letztes Release vor {{n}} Tagen ({{names}}), seither {{count}} PR gemerged",
          unit_other:
            "letztes Release vor {{n}} Tagen ({{names}}), seither {{count}} PRs gemerged",
          threshold_one:
            "Abmachung: mindestens ein Release nach {{prod}} pro Sprint. Gemeldet, wenn mehr als {{n}} Tag kein Release kam.",
          threshold_other:
            "Abmachung: mindestens ein Release nach {{prod}} pro Sprint. Gemeldet, wenn mehr als {{n}} Tage kein Release kam.",
          rule: "Gezählt werden Projekte, die ausgerollt werden und an denen gearbeitet wird. Das erste bekannte Deploy einer App zählt als Release.",
        },
        release_due: {
          title: "Release nach {{prod}} bald fällig",
          unit: "letztes Release vor {{n}} Tagen ({{names}})",
          threshold_one:
            "Abmachung: mindestens ein Release nach {{prod}} pro Sprint. Ein Hinweis, wenn mehr als {{n}} Tag kein Release kam.",
          threshold_other:
            "Abmachung: mindestens ein Release nach {{prod}} pro Sprint. Ein Hinweis, wenn mehr als {{n}} Tage kein Release kam.",
          rule: "Noch innerhalb der Abmachung: ein Hinweis, bevor sie verletzt ist.",
        },
        not_on_prod: {
          title: "Gemergte PRs lange nicht auf {{prod}}",
          unit_one:
            "{{count}} gemergter PR noch nicht auf {{prod}}, seit {{n}} Tagen",
          unit_other:
            "{{count}} gemergte PRs noch nicht auf {{prod}}, der älteste seit {{n}} Tagen",
          threshold_one:
            "Abmachung: höchstens {{n}} Tag vom Merge bis {{prod}}.",
          threshold_other:
            "Abmachung: höchstens {{n}} Tage vom Merge bis {{prod}}.",
          rule: "Gezählt werden PRs von Repos, die ausgerollt werden, ab ihrer Ankunft auf dem Default-Branch; Libraries nicht.",
        },
        slow_to_prod: {
          title: "Lange Wartezeit vom Merge bis {{prod}}",
          unit_one:
            "typisch {{n}} Tage vom Merge bis {{prod}}, aus {{count}} PR",
          unit_other:
            "typisch {{n}} Tage vom Merge bis {{prod}}, aus {{count}} PRs",
          threshold_one:
            "Abmachung: höchstens {{n}} Tag vom Merge bis {{prod}}. Beurteilt ab {{minN}}.",
          threshold_other:
            "Abmachung: höchstens {{n}} Tage vom Merge bis {{prod}}. Beurteilt ab {{minN}}.",
          min_one: "{{n}} PR",
          min_other: "{{n}} PRs",
          rule: "Typischer Wert über die PRs, die im Zeitraum gemerged wurden und bis zu seinem Ende auf {{prod}} kamen.",
        },
        merge_pause: {
          title: "Kaum gemerged, obwohl freigegebene PRs warteten",
          unit_one:
            "KW {{week}}: {{count}} PR gemerged statt typisch {{typical}}, während am Ende der Woche {{n}} freigegebene PRs offen waren ({{names}})",
          unit_other:
            "KW {{week}}: {{count}} PRs gemerged statt typisch {{typical}}, während am Ende der Woche {{n}} freigegebene PRs offen waren ({{names}})",
          unitLost_one:
            "KW {{week}}: {{count}} PR gemerged statt typisch {{typical}} (ohne die Tage, an denen Bitbucket keine Daten lieferte), während am Ende der Woche {{n}} freigegebene PRs offen waren ({{names}})",
          unitLost_other:
            "KW {{week}}: {{count}} PRs gemerged statt typisch {{typical}} (ohne die Tage, an denen Bitbucket keine Daten lieferte), während am Ende der Woche {{n}} freigegebene PRs offen waren ({{names}})",
          threshold:
            "Gemeldet, wenn eine Woche höchstens {{n}} der typischen Merges hat, während am Ende der Woche {{minN}} oder mehr offen waren.",
          min_one: "{{n}} freigegebener PR",
          min_other: "{{n}} freigegebene PRs",
          rule: "Verglichen mit der typischen Woche des Projekts. Freigegeben heisst: Die Freigabe einer Person, nicht des Autors, gilt am Ende der Woche noch, und niemand verlangt Änderungen. Warum nicht gemerged wurde, zeigen die Daten nicht.",
        },
        approved_waiting: {
          title: "Freigegeben, aber nicht gemerged",
          unit_one:
            "{{count}} freigegebener PR wartet auf den Merge, seit {{n}} Arbeitstagen",
          unit_other:
            "{{count}} freigegebene PRs warten auf den Merge, der älteste seit {{n}} Arbeitstagen",
          threshold_one:
            "Gemeldet ab {{minN}} pro Repo mit mehr als {{n}} Arbeitstag Wartezeit auf den Merge.",
          threshold_other:
            "Gemeldet ab {{minN}} pro Repo mit mehr als {{n}} Arbeitstagen Wartezeit auf den Merge.",
          min_one: "{{n}} PR",
          min_other: "{{n}} PRs",
          rule: "Freigegeben heisst: Eine Person, nicht der Autor, hat den PR freigegeben, und die Freigabe gilt noch. Ein PR, für den jemand Änderungen verlangt, wartet nicht auf den Merge.",
        },
        bot_loop: {
          title: "Bot-Branch verbraucht CI-Zeit mit fehlgeschlagenen Builds",
          unit: "{{n}} fehlgeschlagene Builds in Folge, {{ci}} CI-Zeit",
          threshold_one: "Gemeldet ab {{n}} fehlgeschlagenen Build in Folge.",
          threshold_other:
            "Gemeldet ab {{n}} fehlgeschlagenen Builds in Folge.",
          rule: "Verbrauchte CI-Zeit, kein Befund über den Code des Teams.",
        },
        untraced: {
          title: "Nicht messbar, ob gemergte PRs auf {{prod}} sind",
          unit_one:
            "{{n}} von {{of}} gemergten PRs nicht zuordenbar: {{reason}}",
          unit_other:
            "{{n}} von {{of}} gemergten PRs nicht zuordenbar, meist: {{reason}}",
          rule: "Für diese PRs lässt sich nicht sagen, ob und wann sie auf {{prod}} kamen. Die Abmachung zur Zeit bis {{prod}} ist dort nicht messbar.",
        },
        unattributed: {
          title: "Releases nach {{prod}} ohne Projekt",
          unit_one: "{{n}} Release nach {{prod}} keinem Projekt zuordenbar",
          unit_other: "{{n}} Releases nach {{prod}} keinem Projekt zuordenbar",
          rule: "Ihre Änderung im GitOps-Repo ist Bitbucket nicht bekannt. Für ihre Projekte ist die Abmachung «ein Release pro Sprint» darum nicht messbar.",
        },
        no_prod_stage: {
          title: "Argo CD liefert keine Daten zur Stage {{prod}}",
          unit: "Argo CD meldet Deploys nach {{names}}, keines nach {{prod}}",
          rule: "Die Stage-Konfiguration in riptide oder die Benachrichtigung von Argo CD stimmt nicht.",
        },
        deploy_failing: {
          title: "Letztes Deploy nach {{prod}} fehlgeschlagen",
          unit_one:
            "seit {{daysDat}} kein erfolgreiches Deploy, {{count}} Versuch",
          unit_other:
            "seit {{daysDat}} kein erfolgreiches Deploy, {{count}} Versuche",
          threshold_one:
            "Gemeldet ab {{n}} Arbeitstag ohne erfolgreiches Deploy.",
          threshold_other:
            "Gemeldet ab {{n}} Arbeitstagen ohne erfolgreiches Deploy.",
          rule: "Das letzte Deploy der App nach {{prod}} schlug fehl, und seither ist keines gelungen.",
        },
        deploy_failures: {
          title: "Deploys nach {{prod}} scheitern immer wieder",
          unit: "{{n}}-mal scheiterten Deploys hintereinander, jedes Mal bis wieder eines gelang; {{count}} Deploys im Zeitraum",
          threshold: "Gemeldet ab {{n}}-mal im Zeitraum.",
          rule: "Mehrere gescheiterte Versuche hintereinander zählen einmal.",
        },
        first_review_wait: {
          title: "PRs warten auf ein erstes Review",
          unitOldest_one:
            "{{count}} PR wartet auf ein erstes Review, seit {{n}} Arbeitstagen",
          unitOldest_other:
            "{{count}} PRs warten auf ein erstes Review, der älteste seit {{n}} Arbeitstagen",
          unitTypical_one:
            "erstes Review typisch nach {{n}} Arbeitstagen, aus {{count}} PR",
          unitTypical_other:
            "erstes Review typisch nach {{n}} Arbeitstagen, aus {{count}} PRs",
          threshold_one:
            "Gemeldet ab {{minN}} mit mehr als {{n}} Arbeitstag Wartezeit, oder wenn das erste Review typisch so lange dauert.",
          threshold_other:
            "Gemeldet ab {{minN}} mit mehr als {{n}} Arbeitstagen Wartezeit, oder wenn das erste Review typisch so lange dauert.",
          min_one: "{{n}} PR",
          min_other: "{{n}} PRs",
          rule: "PRs, die keine Entwürfe mehr sind und noch keine Reaktion einer Person haben. PRs von Bots nicht gezählt.",
        },
        trunk_failing: {
          title: "Builds auf dem Default-Branch lange fehlgeschlagen",
          unit_one:
            "{{days}} lang fehlgeschlagen, {{count}} fehlgeschlagener Build",
          unit_other:
            "{{days}} lang fehlgeschlagen, {{count}} fehlgeschlagene Builds",
          threshold_one: "Gemeldet ab {{minN}} über mehr als {{n}} Arbeitstag.",
          threshold_other:
            "Gemeldet ab {{minN}} über mehr als {{n}} Arbeitstage.",
          min_one: "{{n}} fehlgeschlagenen Build",
          min_other: "{{n}} fehlgeschlagenen Builds",
          rule: "Vom ersten fehlgeschlagenen Build bis zum nächsten erfolgreichen, pro Default-Branch. Release-Builds nicht gezählt.",
        },
        branch_failing: {
          title: "Builds eines Branches scheitern immer wieder",
          unit: "{{n}} fehlgeschlagene Builds in Folge",
          threshold_one:
            "Gemeldet ab {{n}} fehlgeschlagenen Build in Folge über mindestens {{within}}.",
          threshold_other:
            "Gemeldet ab {{n}} fehlgeschlagenen Builds in Folge über mindestens {{within}}.",
          rule: "Ein Feature- oder Bugfix-Branch einer Person. Branches von Bots nicht gezählt.",
        },
        flaky_builds: {
          title: "Unzuverlässige Builds",
          unit: "{{kind}}: {{n}} von {{of}} fehlgeschlagenen Builds liefen beim nächsten Versuch ohne Änderung durch",
          threshold:
            "Gemeldet ab {{minN}}, wenn solche mindestens {{n}} aller fehlgeschlagenen Builds ausmachen.",
          min_one: "{{n}} solchen Build",
          min_other: "{{n}} solchen Builds",
          rule: "Fehlgeschlagen, dann erfolgreich auf demselben Commit. Eine Untergrenze: Wiederholungen innerhalb eines Builds sieht riptide nicht.",
        },
        long_builds: {
          title: "Builds dauern lang",
          unit_one:
            "{{kind}}: typisch {{n}} Minuten, aus {{count}} erfolgreichen Build",
          unit_other:
            "{{kind}}: typisch {{n}} Minuten, aus {{count}} erfolgreichen Builds",
          threshold_one:
            "Gemeldet, wenn ein erfolgreicher Build typisch länger als {{n}} Minute dauert, aus mindestens {{minN}}.",
          threshold_other:
            "Gemeldet, wenn ein erfolgreicher Build typisch länger als {{n}} Minuten dauert, aus mindestens {{minN}}.",
          min_one: "{{n}} Build",
          min_other: "{{n}} Builds",
          rule: "Typische Dauer der erfolgreichen Builds, pro Repo und Build-Art.",
        },
        release_failing: {
          title: "Release-Builds scheitern",
          unit: "{{count}} von {{of}} Release-Builds fehlgeschlagen ({{n}})",
          threshold:
            "Gemeldet ab {{minN}}, wenn fehlgeschlagene mindestens {{n}} aller Release-Builds ausmachen.",
          min_one: "{{n}} fehlgeschlagenen Release-Build",
          min_other: "{{n}} fehlgeschlagenen Release-Builds",
          rule: "Fehlgeschlagene Release-Builds, pro Repo.",
        },
        builds_silent: {
          title: "CI meldet für ein Repo keine Builds mehr",
          unit_one:
            "{{n}} PRs gemerged, kein Build gemeldet; in den {{withinDat}} davor {{count}} Build",
          unit_other:
            "{{n}} PRs gemerged, kein Build gemeldet; in den {{withinDat}} davor {{count}} Builds",
          threshold_one:
            "Gemeldet ab {{minN}} im letzten {{n}} Arbeitstag ohne gemeldeten Build.",
          threshold_other:
            "Gemeldet ab {{minN}} in den letzten {{n}} Arbeitstagen ohne gemeldeten Build.",
          min_one: "{{n}} gemergten PR",
          min_other: "{{n}} gemergten PRs",
          rule: "Ob dort gebaut wird, zeigen die Daten nicht: Vielleicht meldet die CI nur nicht mehr.",
        },
        big_prs: {
          title: "Viele grosse PRs",
          unit: "{{count}} von {{of}} PRs ändern mehr als {{lines}} Zeilen ({{n}})",
          threshold:
            "Gemeldet, wenn mindestens {{n}} der PRs mehr als {{minN}} ändern.",
          min_one: "{{n}} Zeile",
          min_other: "{{n}} Zeilen",
          rule: "Anteil an den gemergten PRs mit bekannter Grösse.",
        },
        fast_approval: {
          title: "Grosse PRs schnell freigegeben",
          unit_one:
            "{{n}} grosser PR innert {{within}} freigegeben, ohne Kommentar davor",
          unit_other:
            "{{n}} grosse PRs innert {{within}} freigegeben, ohne Kommentar davor",
          threshold_one:
            "Gemeldet ab {{n}} PR über {{minN}}, innert {{within}} freigegeben.",
          threshold_other:
            "Gemeldet ab {{n}} PRs über {{minN}}, innert {{within}} freigegeben.",
          min_one: "{{n}} Zeile",
          min_other: "{{n}} Zeilen",
          rule: "Ohne Kommentar einer Person davor. Die Daten zeigen die Zeit, nicht wie geprüft wurde.",
        },
        unapproved: {
          title: "Ohne Freigabe gemerged",
          unit: "{{count}} von {{of}} gemergten PRs ohne Freigabe einer Person ({{n}})",
          threshold:
            "Gemeldet ab {{minN}} pro Repo, wenn mindestens {{n}} ohne Freigabe sind.",
          min_one: "{{n}} gemergten PR",
          min_other: "{{n}} gemergten PRs",
          rule: "Aufräum-PRs und PRs von Bots nicht gezählt.",
        },
        late_decline: {
          title: "PRs nach langer Arbeit abgelehnt",
          unit_one:
            "{{n}} PR nach mehr als {{withinDat}} abgelehnt ({{names}})",
          unit_other:
            "{{n}} PRs nach mehr als {{withinDat}} abgelehnt ({{names}})",
          threshold_one: "Gemeldet ab {{n}} solchen PR im Team.",
          threshold_other: "Gemeldet ab {{n}} solchen PRs im Team.",
          rule: "Arbeit, die nicht ankam. Nicht gezählt, wenn ein späterer PR vom selben Branch sie weiterführt.",
        },
        branch_life: {
          title: "Branches leben lange, bevor der PR bereit ist",
          unit_one:
            "typisch {{n}} Arbeitstage vom ersten Commit, bis der PR bereit ist, aus {{count}} PR",
          unit_other:
            "typisch {{n}} Arbeitstage vom ersten Commit, bis der PR bereit ist, aus {{count}} PRs",
          threshold_one:
            "Gemeldet ab typisch mehr als {{n}} Arbeitstag, aus mindestens {{minN}}.",
          threshold_other:
            "Gemeldet ab typisch mehr als {{n}} Arbeitstagen, aus mindestens {{minN}}.",
          min_one: "{{n}} PR",
          min_other: "{{n}} PRs",
          rule: "Eine Untergrenze: Ein Rebase setzt den ersten Commit neu.",
        },
        branch_open: {
          title: "Branches ohne PR, lange in Arbeit",
          unit_one:
            "{{count}} Branch ohne PR in Arbeit, seit {{n}} Tagen ({{names}})",
          unit_other:
            "{{count}} Branches ohne PR in Arbeit, der älteste seit {{n}} Tagen ({{names}})",
          threshold_one:
            "Gemeldet ab einem Branch ohne PR, an dem seit mehr als {{n}} Tag gearbeitet wird, zuletzt vor höchstens {{withinDat}} gepusht.",
          threshold_other:
            "Gemeldet ab einem Branch ohne PR, an dem seit mehr als {{n}} Tagen gearbeitet wird, zuletzt vor höchstens {{withinDat}} gepusht.",
          rule: "Branches, die Ziel von PRs sind, und Branches von Bots nicht gezählt. Lag ein Branch so lange still, wie «Branches ohne PR, lange still» meldet, zählt die Arbeit ab dem nächsten Push neu.",
        },
        branch_silent: {
          title: "Branches ohne PR, lange still",
          unit_one: "{{count}} Branch ohne PR nicht mehr gepusht ({{names}})",
          unit_other:
            "{{count}} Branches ohne PR nicht mehr gepusht ({{names}})",
          threshold_one:
            "Gemeldet ab {{minN}} pro Repo, seit mehr als {{n}} Tag nicht gepusht.",
          threshold_other:
            "Gemeldet ab {{minN}} pro Repo, seit mehr als {{n}} Tagen nicht gepusht.",
          min_one: "{{n}} Branch ohne PR",
          min_other: "{{n}} Branches ohne PR",
          rule: "Ob sie noch existieren, meldet Bitbucket nicht.",
        },
        unmeasurable: {
          title: "{{was}}: nicht mehr messbar",
          unit: "",
          rule: "Vor einer Woche traf das hier noch zu; jetzt reichen die Daten nicht, um es zu beurteilen. Das heisst nicht, dass es gelöst ist.",
        },
      },
      tallyLabel: "Bilanz",
      sparkAria: "Pro Woche, Vorperiode: {{prev}}; jetzt: {{cur}}",
      tally: {
        good: "läuft",
        done: "erledigt",
        bad: "hakt",
        hint: "Hinweise",
        limit: "nicht beurteilbar",
        talk: "zum Besprechen",
        ofRepos: "von {{repos}}",
        calm_one: "aktives Repo ohne Befund",
        calm_other: "aktiven Repos ohne Befund",
        unjudged_one: "Repo mit zu wenig Daten",
        unjudged_other: "Repos mit zu wenig Daten",
      },
      calmLine_one: "Von {{count}} aktiven Repo: {{parts}}.",
      calmLine_other: "Von {{count}} aktiven Repos: {{parts}}.",
      calmPart: "{{count}} ohne Befund",
      unjudgedPart: "{{count}} mit zu wenig Daten für ein Urteil",
      idleLine_one:
        "{{n}} Repo ohne Aktivität, das in der Vorperiode aktiv war.",
      idleLine_other:
        "{{n}} Repos ohne Aktivität, die in der Vorperiode aktiv waren.",
      noGain: "Keine Verbesserung, die als Beleg zählt.",
      stuckBelow: "Ohne eigene Karte: Die Befunde stehen weiter unten.",
      waitingAll_one: "{{count}} wartet auf {{what}}",
      waitingAll_other: "alle {{count}} warten auf {{what}}",
      waitingOn: "wartet auf {{what}}: {{count}}",
      allDraft_one: "{{count}} ist ein Entwurf",
      allDraft_other: "alle {{count}} sind Entwürfe",
      seen_one: "schon {{count}} Mal davor",
      seen_other: "schon {{count}} Mal davor",
      kind: {
        data_gap: "Datenlücke",
        pr_jam: "PR-Stau",
        slower_build: "langsamer geworden",
        failing_more: "scheitert öfter",
        change: "wichtige Veränderung",
        agent_wait: "Warten auf Build-Agents",
        resolved: "erledigt",
        question: "Frage",
        state: "Befund",
      },
      title: {
        data_gap: "{{source}} liefert keine Daten",
        pr_jam: "{{repo}}: PRs stauen sich",
        slower_build: "{{repo}}: {{kind}} langsamer als in der Vorperiode",
        failing_more: "{{repo}}: {{kind}} scheitern öfter",
        changeGood: "{{label}}: deutlich besser",
        changeBad: "{{label}}: deutlich schlechter",
        agent_wait: "{{kind}} warten auf Build-Agents",
        resolved: "{{what}}: erledigt",
        question: "{{label}}",
      },
      rule: {
        data_gap:
          "Die Quelle liefert schon vor dem Ende des Zeitraums keine Daten mehr. Ihre Zahlen sagen darum nichts über die Arbeit des Teams.",
        pr_jam:
          "{{min}} oder mehr PRs seit {{days}} Tagen oder länger offen, Entwürfe nicht gezählt. Warum sie liegen, zeigen die Daten nicht: ein Merge-Stopp, eine Abhängigkeit, Ferien oder niemand fühlt sich zuständig.",
        slower_build:
          "Der typische erfolgreiche Build dauert mindestens {{factor}}-mal so lang wie in der Vorperiode. Verglichen wird das Repo mit sich selbst, nicht mit anderen. Was im Build langsamer wurde, zeigen die Daten nicht.",
        failing_more:
          "Builds scheitern viel öfter als in der Vorperiode. Ob Tests, Infrastruktur oder echte Fehler, zeigen die Daten nicht.",
        changeGood:
          "Deutlich besser als in der Vorperiode: über der Schwelle für eine wichtige Veränderung.",
        changeBad:
          "Deutlich schlechter als in der Vorperiode: über der Schwelle für eine wichtige Veränderung. Warum, zeigen die Daten nicht.",
        agent_wait:
          "{{share}} der typischen Build-Zeit oder mehr, und mindestens {{minutes}} Minuten, wartet der Build auf einen freien Build-Agent. Das liegt an der Kapazität der Plattform, nicht am Build.",
        resolved:
          "Die letzte Einschätzung hat es aufgeworfen; jetzt ist der Wert zurück auf dem Stand davor oder besser.",
        resolvedState:
          "Vor einer Woche traf das hier noch zu, jetzt nicht mehr: Der Wert liegt wieder klar unter der Grenze, oder die Merge-Pause oder die Reihe fehlgeschlagener Builds ist vorbei. Die Karte zeigt sie mit ihrem Datum.",
      },
      factsOf: "Fakten zu {{repo}} ansehen →",
      driver: "Vor allem {{repo}}: {{share}} der Zunahme",
      retro: "Retro",
      retroBox: "Für die nächste Retro",
      copy: "Als Markdown kopieren",
      copied: "kopiert",
      questionFixed: {
        feature_share: "Liegt der Anteil Feature-Arbeit da, wo wir ihn wollen?",
        untracked: "PRs ohne Jira-Key: Absicht oder vergessen?",
        wasted_share: "Warum reviewen wir PRs, die wir dann verwerfen?",
        cost_per_kloc: "Ist der Review-Aufwand das wert, was er findet?",
        open_prs: "Mehr Arbeit unterwegs, oder mehr liegen gelassen?",
        first_touch: "Passt das zu unserer Art zu reviewen?",
      },
      also: "Ausserdem erkannt:",
      weeksUnit: "{{n}} Wochen",
      lever: {
        walk_prs:
          "die PRs einzeln durchgehen: reviewen, mergen, schliessen oder bewusst parken",
        ask_why_waiting:
          "klären, worauf sie warten: ein Merge-Stopp, eine Abhängigkeit, oder niemand fühlt sich zuständig",
        inspect_build_log:
          "im Build-Log der langsamsten Builds nachsehen, welcher Schritt die Zeit braucht",
        inspect_failures:
          "die fehlgeschlagenen Builds ansehen: jedes Mal derselbe Fehler oder ein anderer?",
        platform_agents:
          "mit dem Plattform-Team über mehr Build-Agents sprechen",
        find_what_changed:
          "erst klären, was sich geändert hat, dann etwas vereinbaren",
        name_what_helped: "benennen, was geholfen hat, damit es bleibt",
        check_source: "prüfen, wie die Quelle an riptide angebunden ist",
        plan_release:
          "das nächste Release nach {{prod}} planen oder benennen, was es aufhält",
        inspect_deploys:
          "die fehlgeschlagenen Deploys in Argo CD ansehen: jedes Mal derselbe Fehler oder ein anderer?",
        stop_bot_loop:
          "den Bot-Branch ansehen: beheben, woran seine Builds scheitern, oder ihn schliessen",
        walk_branches:
          "die Branches durchgehen: erledigte löschen, einen PR öffnen oder den Rest bewusst parken",
      },
      label: {
        silent_days: "Tage ohne Daten",
        open_long: "lange offene PRs",
        open: "offene PRs",
        age_p50: "typisches Alter",
        failure_rate: "Builds fehlgeschlagen",
        wasted: "CI-Zeit in fehlgeschlagenen Builds",
        runs: "Builds",
        build_p50: "typischer erfolgreicher Build",
        build_wait_p50: "Warten auf einen Build-Agent",
        build_work_p50: "Bauen",
        deploy_per_week: "Releases nach {{prod}} pro Woche",
        deploy_success: "erfolgreiche Deploys",
        lead_time_p50: "Zeit vom ersten Commit bis {{prod}}",
        review_p50: "bereit bis zur ersten Freigabe",
        within_day: "innert eines Tages gemerged",
        jammed_repos: "Repos im Stau",
        build_success: "erfolgreiche Builds",
        merged_per_week: "gemergte PRs pro Woche",
        batch_size: "geänderte Zeilen pro PR",
        wip_weeks: "Arbeit offen",
        feature_share: "Feature-PRs",
        untracked: "PRs ohne Jira-Key",
        wasted_share: "Review-Aufwand für verworfene PRs",
        cost_per_kloc: "Review-Aufwand pro 1000 Zeilen",
        open_prs: "offene PRs",
        first_touch: "Freigaben ohne Kommentar davor",
        state_units: "genannt",
        state_judged: "beurteilt",
        state_threshold: "Schwelle des Teams",
        state_no_release: "Tage seit dem letzten Release",
        state_release_due: "Tage seit dem letzten Release",
        state_not_on_prod: "Tage seit dem Merge des ältesten PRs",
        state_slow_to_prod: "typische Tage bis {{prod}}",
        state_merge_pause: "freigegebene PRs, am Ende der Woche offen",
        state_approved_waiting: "Arbeitstage, die der älteste PR wartet",
        state_bot_loop: "fehlgeschlagene Builds in Folge",
        state_untraced: "PRs nicht zuordenbar",
        state_unattributed: "Releases ohne Projekt",
        state_no_prod_stage: "gemeldete Stages",
        state_deploy_failing: "Arbeitstage ohne erfolgreiches Deploy",
        state_deploy_failures:
          "Male, in denen Deploys hintereinander scheiterten",
        state_first_review_wait: "Arbeitstage ohne erstes Review",
        state_trunk_failing: "Arbeitstage fehlgeschlagen",
        state_branch_failing: "fehlgeschlagene Builds in Folge",
        state_flaky_builds: "unzuverlässige Builds",
        state_long_builds: "Minuten pro erfolgreichem Build",
        state_release_failing: "Release-Builds fehlgeschlagen",
        state_builds_silent: "PRs gemerged ohne gemeldeten Build",
        state_big_prs: "PRs über der Zeilenschwelle",
        state_fast_approval: "grosse PRs schnell freigegeben",
        state_unapproved: "PRs ohne Freigabe",
        state_late_decline: "PRs nach langer Arbeit abgelehnt",
        state_branch_life: "Arbeitstage bis der PR bereit ist",
        state_branch_open: "Tage ohne PR",
        state_branch_silent: "Branches ohne Bewegung",
      },
      waiting: {
        draft: "Entwurf",
        first_review: "erstes Review",
        review: "Review",
        changes: "Änderungen",
        merge: "Merge",
      },
    },
    sinceNote:
      "Argo CD liefert erst seit dem {{date}} Daten: Die Wochen davor sind leer, der Wert pro Woche gilt ab dann.",
    sinceLow: " Weniger als eine Woche Daten: wenig belastbar.",
    throughput:
      "Wie oft, wie schnell und wie verlässlich kommt Code nach {{prod}}?",
    throughputText:
      "Typisch heisst: Die Hälfte der Fälle liegt darüber, die Hälfte darunter.",
    freq: {
      label: "Releases nach {{prod}}",
      dora: "DORA · Deployment frequency",
      question: "Wie oft kommt neuer Code nach {{prod}}?",
      unit: "pro Woche, im Schnitt",
      note_one: "{{count}} Release im Zeitraum",
      note_other: "{{count}} Releases im Zeitraum",
      noDeploys: "keine Deploys im Zeitraum",
      notReporting: "Argo CD liefert für das Team noch keine Daten",
      direction: "öfter ist besser",
      directionWhy: "kleinere Releases, weniger Risiko pro Release",
      firstSeen_one:
        "{{count, num}} erstes Deploy aus den ersten {{days}} Tagen der Argo-CD-Daten nicht gezählt: Ob es eine neue Version brachte, ist unbekannt",
      firstSeen_other:
        "{{count, num}} erste Deploys aus den ersten {{days}} Tagen der Argo-CD-Daten nicht gezählt: Ob sie eine neue Version brachten, ist unbekannt",
      unattributed_one: "{{count, num}} Deploy keinem Projekt zuordenbar",
      unattributed_other: "{{count, num}} Deploys keinem Projekt zuordenbar",
      infra_one: "{{count, num}} Deploy von Infrastruktur-Apps nicht gezählt",
      infra_other:
        "{{count, num}} Deploys von Infrastruktur-Apps nicht gezählt",
      means:
        "Wie oft neuer Code bei den Nutzern ankommt. Häufige kleine Releases tragen wenig Risiko je Release und bringen früh Rückmeldungen.",
      watch:
        "die Wochen stark schwanken oder Releases ausbleiben: Dann sammeln sich Änderungen zu grossen Releases.",
      lever:
        "kleinere Änderungen, automatisierte Deploys, kein fester Release-Termin.",
      def: "Erfolgreiche Deploys nach {{prod}} mit neuer Version: Die Version unterscheidet sich vom vorherigen Deploy derselben App, oder es ist das erste Deploy einer App, die live geht. Eine Änderung im GitOps-Repo, die mehrere Apps aktualisiert, zählt einmal. Geteilt durch die Wochen im Zeitraum, in denen Argo CD Daten lieferte. Deploys von Infrastruktur-Apps zählen nicht: Sie gehören zu keinem Projekt des Teams.",
      basis: "{{releases}}, {{resyncs}} und {{firstSeen}}, aus {{syncs}}.",
      basisFirst_one: " Davon {{count}} Release als erstes Deploy einer App.",
      basisFirst_other:
        " Davon {{count}} Releases als erstes Deploy einer App.",
      filterSucceeded: "nur erfolgreiche Deploys",
      filterStage: "nur {{prod}}",
      filterImages: "nur mit neuer Version",
      filterRevision: "einmal pro Änderung im GitOps-Repo",
      coverageFirst:
        "In den ersten {{days}} Tagen der Argo-CD-Daten hat das erste Deploy einer App keinen Vorgänger: Ob es eine neue Version brachte, ist unbekannt, darum zählt es nicht als Release. Danach zählt das erste Deploy einer App als Release: Die App geht live.",
      coverageAll:
        "Jedes Deploy ist vergleichbar: Das erste Deploy einer App nach den ersten {{days}} Tagen der Argo-CD-Daten zählt als Release.",
      coverageInfra_one:
        " Nicht gezählt: {{count, num}} Deploy von Infrastruktur-Apps, die keinem Projekt des Teams gehören ({{apps}}).",
      coverageInfra_other:
        " Nicht gezählt: {{count, num}} Deploys von Infrastruktur-Apps, die keinem Projekt des Teams gehören ({{apps}}).",
      coverageUnattributed_one:
        " {{count, num}} Deploy ist keinem Projekt zuordenbar: Weder seine Änderung im GitOps-Repo noch ein anderes Deploy seiner App nennt eines. In der Ansicht des ganzen Teams zählt es, in keiner Projektansicht.",
      coverageUnattributed_other:
        " {{count, num}} Deploys sind keinem Projekt zuordenbar: Weder ihre Änderung im GitOps-Repo noch ein anderes Deploy ihrer App nennt eines. In der Ansicht des ganzen Teams zählen sie, in keiner Projektansicht.",
    },
    lead: {
      label: "Zeit vom ersten Commit bis {{prod}}",
      dora: "DORA · Change lead time",
      question:
        "Wie lange dauert es, bis die Arbeit eines PRs bei den Nutzern ankommt?",
      noHuman: "kein gemergter PR bis {{prod}} verfolgt",
      slowest_one: "die langsamsten 10 %: über {{p90}} · aus {{count}} PR",
      slowest_other: "die langsamsten 10 %: über {{p90}} · aus {{count}} PRs",
      directionWhy: "schnellere Rückmeldung von echten Nutzern",
      lowSample: "Weniger als {{n}} PRs: wenig belastbar",
      means:
        "Die Zeit, in der eine fertige Änderung noch keinem Nutzer hilft. Lang heisst: Rückmeldungen kommen spät, Fehler lassen sich schwer einer Änderung zuordnen, Releases werden gross.",
      watch:
        "die langsamsten 10 % weit über dem typischen Wert liegen: Dann bleiben einzelne Änderungen hängen, oft bis zu einem Release-Termin.",
      lever:
        "die Abschnitte, in denen die meiste Zeit liegt. Welche es sind, zeigt «Wo wartet eine Änderung?» weiter unten.",
      def: "Pro PR vom ersten Commit bis zum ersten Deploy nach {{prod}}, das ihn enthält. Gezählt werden die PRs, die im Zeitraum gemerged wurden und bis zu seinem Ende auf {{prod}} waren. PRs von Bots und von Libraries zählen nicht.",
      basis_one:
        "Aus {{count}} PR, gemerged im Zeitraum und bis zu seinem Ende auf {{prod}}.",
      basis_other:
        "Aus {{count}} PRs, gemerged im Zeitraum und bis zu seinem Ende auf {{prod}}.",
      filterBots: "ohne Bots",
      coverage_one:
        "{{traced, num}} von {{count, num}} gemergten PR bis {{prod}} verfolgt: erkannt am Vergleichs-Link im Release-Commit oder an der Version, die ein Build gebaut hat. Ein Release ohne beides fehlt hier.",
      coverage_other:
        "{{traced, num}} von {{count, num}} gemergten PRs bis {{prod}} verfolgt: erkannt am Vergleichs-Link im Release-Commit oder an der Version, die ein Build gebaut hat. Ein Release ohne beides fehlt hier.",
      traced_one:
        "{{traced, num}} von {{count, num}} gemergten PR bis {{prod}} verfolgt",
      traced_other:
        "{{traced, num}} von {{count, num}} gemergten PRs bis {{prod}} verfolgt",
      notYet_one: "{{count, num}} noch nicht auf {{prod}}",
      notYet_other: "{{count, num}} noch nicht auf {{prod}}",
      unmeasured_one:
        "{{count, num}} davon ohne bekannten ersten Commit, nicht in der Zeit eingerechnet",
      unmeasured_other:
        "{{count, num}} davon ohne bekannten ersten Commit, nicht in der Zeit eingerechnet",
    },
    dsucc: {
      label: "Erfolgreiche Deploys",
      dora: "keine DORA-Metrik",
      question: "Klappt der Rollout selbst?",
      unit: "der Deploys",
      note_one: "{{failed}} von {{count}} Deploy fehlgeschlagen",
      note_other: "{{failed}} von {{count}} Deploys fehlgeschlagen",
      direction: "höher ist besser",
      means:
        "Ob der Rollout selbst klappt: Ein abgebrochenes oder fehlgeschlagenes Deploy heisst, jemand muss eingreifen.",
      watch:
        "der Wert sinkt: Dann scheitern Deploys an Konfiguration, Ressourcen oder Abhängigkeiten.",
      lever:
        "Konfiguration, Ressourcen und Abhängigkeiten der Apps; die fehlgeschlagenen Deploys stehen unten unter «Daten», meist ist es dieselbe App oder dieselbe Ursache.",
      def: "Anteil der Deploys nach {{prod}}, die Argo CD als erfolgreich meldet, an allen abgeschlossenen Deploys. Fehlgeschlagene und mit Fehler abgebrochene zählen als fehlgeschlagen. Keine DORA-Metrik: Die Change fail rate zählt Deploys, nach denen jemand eingreifen muss, auch wenn das Deploy selbst gelang.",
      basis:
        "{{finished}} abgeschlossene Deploys, davon {{failed}} fehlgeschlagen.",
      filterFinished: "nur abgeschlossene Deploys",
      coverage:
        "Nur Deploys, die Argo CD gemeldet hat; Deploys von Infrastruktur-Apps zählen nicht.",
    },
    instability: {
      label: "Ärger nach dem Rollout",
      dora: "DORA · Change fail rate · Deployment rework rate · Failed deployment recovery time",
      question:
        "Wie oft macht ein Release Ärger, und wie schnell ist das behoben?",
      value: "nicht gemessen",
      text: "Dafür müsste riptide wissen, welches Release eine Störung auslöste. Diese Quelle fehlt noch.",
    },
  },
  flow: {
    stage: {
      coding: "Entwickeln",
      codingDesc: "erster Commit → PR bereit",
      review: "Review",
      reviewDesc: "PR bereit → erste Freigabe",
      merge: "Merge",
      mergeDesc: "Freigabe → gemerged",
      toPre: "Bis {{pre}}",
      toPreDesc: "gemerged → auf {{pre}}",
      toProdPre: "Von {{pre}} bis {{prod}}",
      toProd: "Vom Merge bis {{prod}}",
      toProdDescPre: "auf {{pre}}, sonst ab Merge → auf {{prod}}",
      toProdDesc: "gemerged → auf {{prod}}",
    },
    bucket: {
      days: "1–3 Tage",
      overDays: "> 3 Tage",
      upTo: "bis {{d}}",
      from: "ab {{d}}",
    },
    chainTitle: "Anteil jedes Abschnitts an der Zeit bis {{prod}}",
    chainSub:
      "Typische Dauer jedes Abschnitts, für die gemergten PRs der Repos, die ausgerollt werden. Es sind dieselben PRs wie bei der Zeit vom ersten Commit bis {{prod}} oben.",
    leadTo: "Vom ersten Commit bis {{env}}:",
    typicalSlowest: "typisch · die langsamsten 10 %: über {{p90}}",
    shareAria: "Anteil an der Zeit bis {{prod}}: {{parts}}",
    ofTime: "· {{share}} der Zeit",
    hot: "Die meiste Zeit liegt in «{{name}}»",
    hotRest: ": {{share}} der Zeit bis {{prod}}.",
    noProd:
      "In diesem Zeitraum liess sich kein PR bis {{prod}} verfolgen. Anteile an der Zeit bis {{prod}} gibt es darum nicht.",
    chainNote_one:
      "Die letzten zwei Abschnitte zählen nur PRs, die sich einem Deploy zuordnen lassen: {{traced, num}} von {{count, num}} gemergten PR bis {{prod}} verfolgt.",
    chainNote_other:
      "Die letzten zwei Abschnitte zählen nur PRs, die sich einem Deploy zuordnen lassen: {{traced, num}} von {{count, num}} gemergten PRs bis {{prod}} verfolgt.",
    notYet_one:
      "{{count, num}} gemergter PR noch nicht auf {{prod}}, gemerged am {{date}}.",
    notYet_other:
      "{{count, num}} gemergte PRs noch nicht auf {{prod}}, der älteste gemerged am {{date}}.",
    notPlaceable_one:
      "{{count, num}} gemergter PR nicht zuordenbar: {{reasons}}.",
    notPlaceable_other:
      "{{count, num}} gemergte PRs nicht zuordenbar: {{reasons}}.",
    reasonPart: "{{reason}} ({{count, num}})",
    lowerBound_one:
      "{{count, num}} verfolgter PR kam vielleicht schon mit einem früheren Deploy auf {{prod}}, das sich keinem PR zuordnen lässt: Seine Zeit bis {{prod}} ist ein Höchstwert.",
    lowerBound_other:
      "{{count, num}} verfolgte PRs kamen vielleicht schon mit einem früheren Deploy auf {{prod}}, das sich keinem PR zuordnen lässt: Ihre Zeit bis {{prod}} ist ein Höchstwert.",
    library_one:
      "{{count, num}} PR in Libraries, wird nicht ausgerollt und zählt bei der Zeit bis {{prod}} nicht mit.",
    library_other:
      "{{count, num}} PRs in Libraries, werden nicht ausgerollt und zählen bei der Zeit bis {{prod}} nicht mit.",
    perRepo_one: "Pro Repo ({{count}} Repo)",
    perRepo_other: "Pro Repo ({{count}} Repos)",
    colRepo: "Repo",
    colMerged: "Gemerged",
    colOnProd: "Auf {{prod}}",
    colNotYet: "Noch nicht auf {{prod}}",
    colNotPlaceable: "Nicht zuordenbar",
    colLibrary: "Library",
    oldestSince: "ältester gemerged am {{date}}",
    ridge: {
      title: "Wie die Zeit in jedem Abschnitt verteilt ist",
      sub: "Dieselben PRs wie oben, die Zeitachse logarithmisch. Durchgezogener Strich: typisch; gestrichelt: ab da die langsamsten 10 %.",
      caption: "Wartezeit je Abschnitt",
      colStage: "Abschnitt",
      few: "unter {{min}} PRs: einzelne PRs statt Kurve",
      none: "keine PRs",
      tipN: "PRs",
      tipTypical: "typisch",
      tipSlowest: "die langsamsten 10 %: über",
      tick: {
        min: "{{n}} min",
        h: "{{n}} h",
        d: "{{n}} T",
        w: "{{n}} W",
      },
      note: "Ganz links zählen auch kürzere Wartezeiten mit, ganz rechts auch längere.",
    },
    people: "Menschen",
    eyebrow:
      "Vom ersten Commit nach {{prod}} · nur Repos, die ausgerollt werden",
    title: "Wo wartet eine Änderung?",
    lead: "Die Zeit bis zum Nutzer, aufgeteilt in Abschnitte.",
    fwLeadTime: "Change lead time, aufgeschlüsselt",
    reviewTitle: "Review: schnell, aber wie gründlich?",
    review: {
      label: "Zeit bis zur Freigabe",
      question: "Wie lange wartet ein PR auf die erste Freigabe?",
      note_one: "die langsamsten 10 %: über {{p90}} · {{count}} PR",
      note_other: "die langsamsten 10 %: über {{p90}} · {{count}} PRs",
      means:
        "Wie lange ein fertiger PR auf die erste Freigabe wartet. In dieser Zeit liegt die Arbeit still, und der Autor beginnt oft schon etwas Neues.",
      watch:
        "die langsamsten 10 % weit über dem typischen Wert liegen: Einzelne PRs bleiben liegen, oft grosse oder solche ohne klaren Reviewer.",
      lever:
        "Ob das Team Review-Zeiten vereinbart hat, wie klein die PRs sind und ob beim Öffnen gleich ein Reviewer genannt wird.",
      def: "Vom Moment, in dem der PR bereit ist (geöffnet, bei einem Entwurf: als bereit markiert), bis zur ersten Freigabe durch einen Reviewer, der ein Mensch ist.",
      basis_one: "{{count}} PR mit Freigabe.",
      basis_other: "{{count}} PRs mit Freigabe.",
      filterReviewer: "Reviewer ist nicht der Autor",
      coverage: "Vollständig, soweit Bitbucket Daten lieferte.",
    },
    touch: {
      label: "Freigabe ohne Kommentar",
      question: "Wie oft ist die Freigabe die erste Reaktion des Reviewers?",
      directionWhy: "ein Anlass zum Gespräch",
      means:
        "Wie oft die Freigabe die erste Reaktion des Reviewers ist. Hoch heisst: Review ohne sichtbare Diskussion in Bitbucket.",
      watch:
        "der Wert zusammen mit sehr kurzen Review-Zeiten steigt: Dann wird vielleicht durchgewinkt.",
      lever:
        "Diskussionen ausserhalb von Bitbucket, Pairing, kleine PRs, die keinen Kommentar brauchen.",
      def: "Anteil der freigegebenen PRs, bei denen die Freigabe der erste Kontakt des Reviewers mit dem PR ist.",
      basis: "{{n}} von {{total}} freigegebenen PRs.",
      filterComments: "ohne Kommentare von Bots",
      coverage: "Mündliche Reviews und Pairing sieht riptide nicht.",
    },
    cycle: {
      label: "Innert 24 h gemerged",
      question: "Wie viele PRs sind innert 24 h nach dem Öffnen gemerged?",
      unit_one: "{{n}} von {{count}} PR",
      unit_other: "{{n}} von {{count}} PRs",
      direction: "mehr ist besser",
      directionWhy: "kleine PRs, schnelles Review",
      means:
        "Wie viele PRs innert 24 Stunden nach dem Öffnen gemerged sind. Danach ist der Autor meist schon bei der nächsten Aufgabe; Rückfragen kosten dann doppelt.",
      watch:
        "viele PRs über {{days}} Tage offen sind: Das sind meist grosse PRs oder solche, die auf jemanden warten.",
      lever: "kleinere PRs, früh angefragte Reviews.",
      def: "Pro PR die Zeit vom Öffnen bis zum Merge. Typisch dauert das {{p50}}, die langsamsten 10 % über {{p90}}.",
      basis: "{{count}} von {{n}} PRs von Menschen, gemerged im Zeitraum.",
      filterMerged: "im Zeitraum gemerged",
      coverage:
        "PRs, die vor dem Beginn der Daten geöffnet wurden, fehlen: Wann sie geöffnet wurden, ist nicht bekannt. Beim Öffnen gesehen: {{seen, num}} von {{total, num}}, die gemerged wurden.",
    },
    histHead: "PRs nach Dauer vom Öffnen bis zum Merge",
    histLine: " · die Linie markiert 24 h",
    histAria: "Dauer vom Öffnen bis zum Merge: {{parts}}",
  },
  throughput: {
    head: "Wie viel Arbeit wird fertig, und in welchen Portionen?",
    scope:
      "Nur PRs von Menschen: ohne Bots und ohne PRs, die nur eine Version auf die nächste Stage heben.",
    merged: {
      label: "Gemergte PRs pro Woche",
      framework: "Flow Framework · Flow velocity",
      question:
        "Wie viele PRs schliesst das Team pro Woche mit einem Merge ab?",
      unit: "pro Woche, im Schnitt",
      note_one: "{{count, num}} PR im Zeitraum gemerged",
      note_other: "{{count, num}} PRs im Zeitraum gemerged",
      housekeeping_one: "{{count, num}} Aufräum-PR nicht gezählt",
      housekeeping_other: "{{count, num}} Aufräum-PRs nicht gezählt",
      housekeepingTitle:
        "Derselbe Titel in {{repos}} oder mehr Repos innert {{minutes}} Minuten eröffnet, ohne Freigabe einer Person und ohne Jira-Key: Pflege mit einem Werkzeug über alle Repos, keine Arbeit am Projekt.",
      umbrella_one: "{{count, num}} Merge eines Sammel-Branches nicht gezählt",
      umbrella_other: "{{count, num}} Merges von Sammel-Branches nicht gezählt",
      umbrellaTitle:
        "Der PR brachte nur die Arbeit anderer PRs auf den Default-Branch: Sie wurden in seinen Branch gemerged oder mit ihm. Diese PRs zählen, er selbst nicht.",
      twice_one: "{{count, num}} PR doppelt, nicht gezählt",
      twice_other: "{{count, num}} PRs doppelt, nicht gezählt",
      twiceTitle:
        "Derselbe Branch kam schon mit einem anderen PR in einen Sammel-Branch und zählt dort. Als der Sammel-Branch gemerged wurde, galt dieser PR mit als gemerged: dieselbe Arbeit, einmal gezählt.",
      since:
        "Bitbucket liefert erst seit dem {{date}} Daten: Die Wochen davor sind leer, der Wert pro Woche gilt ab dann.",
      direction: "mehr ist besser",
    },
    batch: {
      label: "PR-Grösse",
      framework: "DORA-Capability · Working in small batches",
      question: "Wie gross ist ein typischer PR?",
      unit: "geänderte Zeilen pro PR, typisch",
      note: "die grössten 10 %: ab {{p90}} Zeilen · typisch {{files}} Dateien · Grösse bekannt bei {{n}} von {{total}} PRs",
      none: "Keine PR-Grössen im Zeitraum.",
      direction: "kleiner ist besser",
    },
  },
  open: {
    title: "Offene PRs: wo staut es sich?",
    sub: "Stand {{at}}, egal wann geöffnet. Ab {{days}} Tagen offen gilt ein PR als lang offen, ab {{min}} lang offenen im selben Repo staut es sich; ein Entwurf nie, er darf liegen. Ohne GitOps-Repos und Bots.",
    jammed: {
      label: "Repos im Stau",
      question:
        "In wie vielen Repos sind {{min}} oder mehr PRs seit {{days}} Tagen oder länger offen?",
      unit_one: "mit {{count}} lang offenen PR",
      unit_other: "mit {{count}} lang offenen PRs",
      note_one: "insgesamt {{count}} PR lang offen",
      note_other: "insgesamt {{count}} PRs lang offen",
      direction: "weniger ist besser",
      directionWhy:
        "mehrere liegengebliebene PRs im selben Repo sind ein Stau; ein einzelner ist ein Einzelfall",
    },
    all: {
      label: "Offene PRs",
      question: "Wie viele PRs sind gerade offen?",
      unit: "typisch {{p50}} alt",
      note: "die ältesten 10 %: über {{p90}}",
      directionWhy: "mehr offene PRs kann auch mehr Arbeit im Gang heissen",
    },
    longWaitOn: "Die lang offenen warten auf:",
    inactive_one:
      "{{count}} PR inaktiv: seit {{days}} Tagen nichts mehr in Bitbucket, darum in keiner Zahl oben. Ein Merge, den Bitbucket nicht meldete, sähe gleich aus.",
    inactive_other:
      "{{count}} PRs inaktiv: seit {{days}} Tagen nichts mehr in Bitbucket, darum in keiner Zahl oben. Ein Merge, den Bitbucket nicht meldete, sähe gleich aus.",
    unknown_one:
      "{{count, num}} PR mit unbekanntem Zustand: Zuletzt gesehen vor einem Ausfall, in dem Bitbucket keine Daten lieferte; ob er seither gemerged oder geschlossen wurde, ist unbekannt. In keiner Zahl oben.",
    unknown_other:
      "{{count, num}} PRs mit unbekanntem Zustand: Zuletzt gesehen vor einem Ausfall, in dem Bitbucket keine Daten lieferte; ob sie seither gemerged oder geschlossen wurden, ist unbekannt. In keiner Zahl oben.",
    viewInactive: "Ansehen →",
    jam: "Stau",
    colRepo: "Repo",
    colOpen: "Offen",
    colLong: "≥ {{days}} T.",
    colMedian: "Alter, typisch",
    colWaiting: "Wartet auf",
    colInactive: "Inaktiv",
    viewPRs: "PRs ansehen →",
    none: "Gerade ist kein PR offen.",
    mekko: {
      head: "Wer wartet worauf: Breite = offene PRs im Repo, Höhe = Anteil pro Wartegrund, inaktive nicht dabei.",
      aria: "Offene PRs pro Repo, nach dem, worauf sie warten",
      of_one: "{{n}} von {{open}} offenen PR",
      of_other: "{{n}} von {{open}} offenen PRs",
      long: "{{long}} davon ≥ {{days}} Tage offen",
      longMark: "≥ {{days}} Tage offen",
      draft: "wartet nicht aufs Team",
    },
    why: {
      means:
        "Jede andere PR-Zahl zählt einen PR erst, wenn er gemerged ist. Ein PR, der seit Wochen offen ist, fehlt dort ganz; hier zeigt er sich, pro Repo und mit dem, worauf er wartet.",
      watch:
        "mehrere lang offene PRs im selben Repo auf ein erstes Review warten: Dort fehlt ein Reviewer, oder die PRs sind zu gross.",
      lever:
        "Ob beim Öffnen ein Reviewer genannt wird, ob alte PRs im Daily zur Sprache kommen und ob PRs, die niemand mehr braucht, geschlossen werden.",
      def: "PRs, die jetzt offen sind, auch die in der laufenden Woche geöffneten; verglichen mit denen, die am Ende der Vorperiode offen waren. Das Alter zählt ab dem letzten Wechsel auf «bereit für Review», sonst ab dem Öffnen: Zeit als Entwurf zählt nicht, ein Entwurf hat kein Alter. Lang offen ab {{days}} Tagen, Stau ab {{min}} lang offenen im selben Repo. Ein Entwurf ist nie lang offen und nie vergessen: Er wartet auf seinen Autor. Pro Repo das typische Alter, nie das des ältesten: Ein einzelner PR soll es nicht bestimmen.",
      basis_one: "{{count}} offener PR, Stand {{at}}.",
      basis_other: "{{count}} offene PRs, Stand {{at}}.",
      coverage:
        "Ein PR, dessen Merge oder Schliessen Bitbucket nie gemeldet hat, bleibt offen, bis er nach {{inactive}} Tagen ohne Ereignis als inaktiv zählt; allein macht er nie einen Stau. Kam sein letztes Ereignis vor einem Ausfall, in dem Bitbucket keine Daten lieferte, obwohl Builds liefen, und seither keines mehr, ist sein Zustand unbekannt: Er zählt nicht als offen. Ein PR ohne jedes Ereignis seit Beginn der Daten fehlt.",
    },
  },
  deploys: {
    eyebrow: "Rollout",
    title: "Wie gleichmässig kommt Code nach {{prod}}?",
    fwSpeed: "Speed, sekundär",
    cardTitle: "Releases nach {{prod}} pro Woche",
    cardSub:
      "Nur Releases, also Deploys mit neuer Version; die Linie ist die typische Woche des Teams",
    release: "Release",
    releases: "Releases",
    typical: "typische Woche: {{n}}",
    aria: "Releases nach {{prod}} pro Woche: {{parts}}",
    note: "{{resyncs}} zählen nicht. {{firstSeen}} aus den ersten {{days}} Tagen der Argo-CD-Daten zählen auch nicht: Ob sie eine neue Version brachten, ist offen. Die typische Woche ist der mittlere Wert der ganzen Wochen, in denen Argo CD Daten lieferte.",
  },
  calendar: {
    cardTitle: "Releases nach {{prod}} pro Tag",
    cardSub:
      "Jedes Release am Tag seines ersten Deploys; rechts je Wochentag die Releases pro ganzen Tag mit Daten",
    none: "kein Release",
    four: "4 und mehr",
    noData: "keine Daten",
    weekdayCell_one: "{{perDay}}: {{releases}} an {{count}} Tag",
    weekdayCell_other: "{{perDay}}: {{releases}} an {{count}} Tagen",
    weekdayTitle_one:
      "{{day}}: {{releases}} Releases an {{count}} ganzen Tag mit Daten",
    weekdayTitle_other:
      "{{day}}: {{releases}} Releases an {{count}} ganzen Tagen mit Daten",
    noDataDays_one: "{{count}} Tag ohne Daten",
    noDataDays_other: "{{count}} Tage ohne Daten",
    perDay: "pro ganzen Tag mit Daten",
    tableCaption: "Releases nach {{prod}} pro Tag; {{noData}}",
    week: "Woche",
    days: {
      mon: "Mo",
      tue: "Di",
      wed: "Mi",
      thu: "Do",
      fri: "Fr",
      sat: "Sa",
      sun: "So",
    },
  },
  mix: {
    feature: "Feature",
    bugfix: "Bugfix",
    hotfix: "Hotfix",
    other: "Anderes / ohne Präfix",
    eyebrow: "Arbeitsmix",
    title: "Woran arbeitet das Team?",
    lead: "Nach Präfix des Branches, als Anteil an den PRs, nicht an der Zeit.",
    fwImpact: "Impact, Näherung für «Zeit für Neues»",
    cardTitle: "PRs nach Art, gemerged im Zeitraum",
    cardSub_one: "{{count}} PR im Zeitraum, ohne Bots",
    cardSub_other: "{{count}} PRs im Zeitraum, ohne Bots",
    aria: "Arbeitsmix: {{parts}}",
    barTitle_one: "{{label}}: {{count}} PR, {{share}}",
    barTitle_other: "{{label}}: {{count}} PRs, {{share}}",
    untracked: "{{rate}} ohne Jira-Key",
    untrackedText:
      "({{n}} von {{total}}): Key vergessen oder ungeplante Arbeit?",
  },
  heat: {
    title: "Anteil fehlgeschlagener Builds pro Repo und Woche",
    sub: "Eine rote Zeile heisst: dieses Repo. Eine rote Spalte heisst: alle gleichzeitig, eher die Infrastruktur.",
    aria: "Anteil fehlgeschlagener Builds pro Repo und Woche, {{kind}}",
    failed_one: "{{failed}} von {{count}} Build fehlgeschlagen",
    failed_other: "{{failed}} von {{count}} Builds fehlgeschlagen",
    noVerdict: "unter {{min}} Builds: kein Urteil",
    none: "keine Builds",
    zero: "0 %",
    below: "unter {{v}}",
    from: "ab {{v}}",
    other: "übrige Repos",
    colRepo: "Repo",
  },
  build: {
    kind: terms.de.runKind,
    kindNote: {
      release: "Builds, die die CI als Release oder Hotfix meldet",
      main: "Default-Branch (master oder main), kein Release",
      feature:
        "feature/, feat/, bugfix/, fix/, hotfix/, chore/, refactor/ und Builds von PRs",
      other:
        "docs/ und doc/ (oft ohne Build), release/… ohne Release, nächtliche Builds, eigene Namensschemas",
      unknown: "bevor die CI meldete, welche Art Build es ist",
    },
    prefixes: "Häufigste Präfixe",
    noBranch: "ohne Branch im Pipeline-Namen",
    featureMissingTitle: "Feature- und Bugfix-Builds: kein Build erkannt",
    featureMissing_one:
      "Erkannt werden Branches mit feature/, feat/, bugfix/, fix/, hotfix/, chore/, refactor/ und Builds von PRs. {{count}} Build auf einem Branch ohne bekanntes Präfix steht unter «Übrige Branches» (release/, docs/, doc/, nächtliche Builds und Bots nicht mitgezählt), vermutlich ein Feature-Build des Teams.",
    featureMissing_other:
      "Erkannt werden Branches mit feature/, feat/, bugfix/, fix/, hotfix/, chore/, refactor/ und Builds von PRs. {{count}} Builds auf Branches ohne bekanntes Präfix stehen unter «Übrige Branches» (release/, docs/, doc/, nächtliche Builds und Bots nicht mitgezählt), vermutlich die Feature-Builds des Teams.",
    sparkAria: "Anteil erfolgreicher Builds pro Woche, {{kind}}: {{parts}}",
    successful: "erfolgreich",
    typicalDuration: "typischer erfolgreicher Build, alle Repos",
    repos:
      "Von {{compared}} Repos sind {{slower}} langsamer, {{failing}} fehleranfälliger geworden als in der Vorperiode",
    reposNone: "Repos: kein Vergleich mit der Vorperiode",
    redRuns: "in fehlgeschlagenen Builds, von {{h}} CI-Zeit",
    waitingTitle:
      "Typischer Wert über {{n}} von {{total}} Builds: Nur diese melden, wann sie einen Build-Agent bekamen",
    waiting:
      "davon typisch {{wait}} Warten auf einen Build-Agent, {{build}} Bauen",
    scale: "KW {{kw}} · erfolgreich, Skala {{low}}–100 %",
    runs_one: "{{count}} Build",
    runs_other: "{{count}} Builds",
    noVerdict: " + {{n}} ohne Ergebnis",
    outliers: " · {{n}} Ausreisser ({{h}}) nicht in Dauer und CI-Zeit",
    flaky_one:
      "{{count}} fehlgeschlagener Build unzuverlässig: danach auf demselben Commit erfolgreich ({{share}} der fehlgeschlagenen, {{h}})",
    flaky_other:
      "{{count}} fehlgeschlagene Builds unzuverlässig: danach auf demselben Commit erfolgreich ({{share}} der fehlgeschlagenen, {{h}})",
    flakyNone:
      "Kein fehlgeschlagener Build war danach auf demselben Commit erfolgreich",
    flakyNoRed: "Kein fehlgeschlagener Build, also auch kein unzuverlässiger",
    unjudged_one: " · {{count}} Build ohne Commit nicht beurteilt",
    unjudged_other: " · {{count}} Builds ohne Commit nicht beurteilt",
    flakyRate: "Unzuverlässig: {{rate}} der Builds mit Commit",
    viewFlaky: "Unzuverlässige Builds ansehen →",
    flakyTitle:
      "Unzuverlässig: Ein Build schlägt fehl, ein späterer Build derselben Pipeline auf demselben Commit ist erfolgreich. Eine Untergrenze: Wiederholungen innerhalb eines Builds, nur einmal gebaute Commits und Wiederholungen nach dem Ende des Zeitraums fehlen. Ob Tests oder Infrastruktur scheiterten, zeigen die Daten nicht.",
    repoFlaky: "{{n}} unzuverlässig",
    repoFlakyTitle:
      "{{h}} davon in Builds, die ein späterer Build auf demselben Commit erfolgreich machte",
    failingMore: "Fehleranfälliger geworden",
    failingMoreText: "Vorperiode {{prev}} → jetzt {{rate}} fehlgeschlagen",
    slow: "Langsamer geworden",
    slowText: "Vorperiode {{prev}} → jetzt {{p50}} ({{factor}}×)",
    reliable: "Verlässlicher geworden",
    faster: "Schneller geworden",
    fasterText: "Vorperiode {{prev}} → jetzt {{p50}}",
    fine: "Ohne Befund",
    noCompare: "Kein Vergleich",
    noCompareNoValue:
      "keine vergleichbaren Builds dieser Art in der Vorperiode",
    noCompareIncomplete: "Build-Daten der Vorperiode unvollständig",
    noCompareStarted: "{{source}} liefert erst seit dem {{date}} Daten",
    noCompareNotReporting: "{{source}} liefert keine Daten",
    noCompareLost: "Bitbucket lieferte vom {{from}} bis {{to}} keine Daten",
    noCompareLostOngoing: "Bitbucket liefert seit dem {{from}} keine Daten",
    noCompareLowSample:
      "unter {{min}} Builds im Zeitraum oder in der Vorperiode",
    fineText: "{{p50}}, {{rate}} fehlgeschlagen",
    fineWait: " · {{wait}} Warten, {{build}} Bauen",
    repoWait: "Typisch {{wait}} Warten auf einen Build-Agent, {{build}} Bauen",
    group_one: "{{count}} Build aus {{repos}} Repos startete {{when}}",
    group_other: "{{count}} Builds aus {{repos}} Repos starteten {{when}}",
    groupSameDay: "am {{day}} zwischen {{from}} und {{to}}",
    groupSpan: "zwischen {{from}} und {{to}}",
    groupRest:
      ": Das spricht für die Infrastruktur (Build-Agents weg, ein Schritt, der auf eine Eingabe wartet), nicht für die Repos.",
    colPipeline: "Pipeline",
    colKind: "Art",
    colStart: "Start",
    colDuration: "Dauer",
    colWaiting: "Warten",
    colFactor: "× typisch",
    noAgent: "Der Build meldet nicht, wann er einen Build-Agent bekam",
    eyebrow: "CI · Jenkins · Tekton · jede Build-Art für sich",
    title: "Wie schnell und verlässlich ist das Feedback?",
    fwEffectiveness: "Effectiveness, Treiber",
    dataFrom: "Build-Daten ab {{date}}; die Wochen davor sind leer.",
    unattributed_one:
      "{{count, num}} Build ohne Repo: Weder sein Commit noch sein Pipeline-Name nennt eines. In der Ansicht des ganzen Teams gezählt, in keiner Projektansicht.",
    unattributed_other:
      "{{count, num}} Builds ohne Repo: Weder ihr Commit noch ihr Pipeline-Name nennt eines. In der Ansicht des ganzen Teams gezählt, in keiner Projektansicht.",
    botRuns_one: "Bot-Builds: {{count, num}} Build, {{h}} CI-Zeit",
    botRuns_other: "Bot-Builds: {{count, num}} Builds, {{h}} CI-Zeit",
    botRunsFailed_one: ", davon {{count, num}} fehlgeschlagen ({{h}})",
    botRunsFailed_other: ", davon {{count, num}} fehlgeschlagen ({{h}})",
    botRunsRest:
      ". Kosten, kein Befund: in keiner anderen Zahl dieses Abschnitts.",
    noRuns: "Keine abgeschlossenen Builds im Zeitraum.",
    legend:
      "Erfolgreich: SUCCESS (Tekton: Succeeded, Completed). Fehlgeschlagen: FAILURE, UNSTABLE oder Failed. Ohne Ergebnis (ABORTED, NOT_BUILT): kein Build, der zählt, nur CI-Zeit; ein Timeout in Jenkins endet ebenfalls als ABORTED.",
    hotspotsTitle: "Repos, die das Feedback bremsen",
    hotspotsSub:
      "Jedes Repo gegen seine eigenen Builds derselben Art in der Vorperiode",
    kindGroup: "Build-Art",
    colRepo: "Repo",
    colWhy: "Warum",
    colRed: "CI-Zeit fehlgeschlagen",
    viewRuns: "Builds ansehen →",
    noneOfKind: "Keine Builds dieser Art mit Ergebnis.",
    slowestTitle: "Längste Builds",
    slowestSub:
      "Nach typischer Dauer der erfolgreichen Builds, ohne Ausreisser, ab {{min}} davon.",
    colMedian: "Typisch",
    colSlowest: "Langsamste 10 %",
    colWaitBuild: "Warten / Bauen",
    colRuns: "Builds",
    noAgentRepo: "Keiner der Builds meldet, wann er einen Build-Agent bekam",
    hotspotsRule:
      "Fehleranfälliger geworden: Der Anteil fehlgeschlagener Builds ist mindestens {{failing}}-mal so hoch wie in der Vorperiode und {{points}} Prozentpunkte darüber (bei schon hohem Anteil genügt die halbe Strecke bis 100 %), mit mindestens {{failures}} fehlgeschlagenen Builds. Langsamer geworden: Die typische Dauer ist mindestens {{slow}}-mal so lang wie in der Vorperiode. Beides ab {{min}} Builds im Zeitraum und in der Vorperiode.",
    stuckTitle: "Hängende Builds",
    stuckSub:
      "Builds über {{factor}}× der typischen Dauer ihrer Art und über {{min}}",
    stuck_one: "1 Build hing",
    stuck_other: "{{count}} Builds hingen",
    stuckRest: " im Zeitraum. Ihre Zeit zählt weder in Dauer noch CI-Zeit.",
    showLongest: "Die längsten {{n}} anzeigen",
    showAll: "Builds anzeigen",
    noOutliers: "Keine Ausreisser im Zeitraum.",
    stuckRule:
      "Ein Repo mit mindestens {{min}} eigenen erfolgreichen Builds muss zudem {{factor}}× seine eigene typische Dauer überschreiten. Abgebrochene Builds erscheinen trotzdem.",
    flow: {
      title: "Wohin die CI-Stunden fliessen",
      sub: "CI-Zeit jeder Build-Art nach Ausgang, Ausreisser eingerechnet{{sep}}Bandbreite = Stunden",
      outcome: {
        succeeded: "Erfolgreich",
        flaky: "Unzuverlässig (danach erfolgreich)",
        failed: "Fehlgeschlagen",
        no_verdict: "Ohne Ergebnis",
        outlier: "Ausreisser",
      },
      tipHours: "CI-Zeit",
      tipOfKind: "Anteil an {{kind}}",
      tipOfAll: "Anteil an aller CI-Zeit",
      aria: "CI-Zeit von der Build-Art zum Ausgang: {{parts}}",
      withOutliers: "{{h}} inkl. Ausreisser",
      repos: "Nach Repo, über alle Build-Arten",
      reposMeta:
        "schraffiert: fehlgeschlagen (unzuverlässige eingeschlossen), ohne Ausreisser",
      repoFailed: "{{repo}}: {{h}}, davon {{failed}} fehlgeschlagen",
      repoFailedShort: " · {{h}} fehlgeschlagen",
      reposOther_one: "{{count}} weiteres",
      reposOther_other: "{{count}} weitere",
      reposAria: "CI-Zeit nach Repo: {{parts}}",
      rule: "Fehlgeschlagen: FAILURE, UNSTABLE, Failed. Unzuverlässig: ebenso fehlgeschlagen, doch ein späterer Build derselben Pipeline war auf demselben Commit erfolgreich. Ohne Ergebnis: ABORTED, NOT_BUILT, bei Tekton None, Timeouts in Jenkins eingeschlossen. Ausreisser: {{outliers}} (ein Repo mit genug eigenen Builds zudem über seiner eigenen typischen Dauer). Ein Ausreisser zählt nur als Ausreisser, egal mit welchem Ergebnis; darum sind hier die Stunden der Ausreisser dabei, die die Karten oben weglassen. Die Bänder jeder Art zeigen ihre eigene Aufteilung, die Höhe eines Knotens nur die Menge: kein Vergleich zwischen den Arten.",
    },
  },
  quiver: {
    aria: "{{kind}}: jedes Repo von der Vorperiode zu jetzt, typische Dauer gegen Anteil fehlgeschlagener Builds",
    xAxis: "Typische Dauer (log) →",
    yAxis: "↑ Anteil fehlgeschlagen",
    slower: "▲ ×{{factor}} Dauer",
    failingMore: "▲ fehlgeschlagen {{prev}} → {{rate}}",
    median: "Typisch",
    rate: "Anteil fehlgeschlagen",
    runs: "Builds",
    ci: "CI-Zeit",
    legend:
      "Pfeil: Vorperiode → jetzt; ein Punkt im oder am Kreis: kaum bewegt · voller Kreis ohne Punkt: nur die typische Dauer oder nur der Anteil fehlgeschlagener Builds vergleichbar · Kreisfläche: Anteil an der CI-Zeit dieser Art · rot: langsamer oder fehleranfälliger geworden, wie unten · gestrichelt: kein Vergleich.",
    unplotted_one:
      "{{count}} Repo dieser Art ist nicht eingezeichnet: ausserhalb der Liste, ohne Build mit Ergebnis oder ohne typische Dauer.",
    unplotted_other:
      "{{count}} Repos dieser Art sind nicht eingezeichnet: ausserhalb der Liste, ohne Build mit Ergebnis oder ohne typische Dauer.",
  },
  costs: {
    eyebrow: "FinOps · noergler",
    title: "Was kosten die KI-Reviews?",
    lead: "Ausgaben der automatischen Reviews im Zeitraum.",
    spend: "Ausgaben",
    spendNote_one: "davon {{merged}} für {{count}} PR, der gemerged wurde",
    spendNote_other: "davon {{merged}} für {{count}} PRs, die gemerged wurden",
    promotionNote_one: "{{promotion}} für {{count}} Promotion-Review",
    promotionNote_other: "{{promotion}} für {{count}} Promotion-Reviews",
    wasted: "Davon verworfen",
    wastedQuestion: "Wie viel ging an PRs, die nie gemerged wurden?",
    wastedWhy: "hoch heisst: Viele Entwürfe werden reviewt und dann verworfen",
    perKloc: "Pro 1000 geänderte Zeilen",
    perKlocQuestion:
      "Was kostet das Review einer Änderung, die gemerged wurde?",
    trendOnly: "nur im Verlauf lesen",
    perKlocWhy: "steigt der Wert, werden Reviews pro Änderung teurer",
    byProduct: "Nach Projekt",
    byRepo: "Nach Repo",
    sub: "Alle Reviews im Zeitraum, der letzte Stand pro PR",
    none: "Keine noergler-Reviews im Zeitraum.",
    reviewedRepos: "Nur Repos, die noergler reviewt: {{n}} im Zeitraum.",
  },
  facts: {
    eyebrow: "Einzelfakten hinter allen Zahlen",
    title: "Daten",
    searchLabel: "Suche nach Repo, App, PR oder Jira-Key",
    placeholder: "Repo, App, PR, Jira-Key …",
    failed:
      "Die Einzelfakten konnten nicht geladen werden; die Meldung steht oben.",
    tabs: "Faktentabelle",
    tabPRs: "PRs",
    tabDeploys: "Deploys",
    tabRuns: "Builds",
    filtered: "gefiltert nach «{{q}}»",
    newest: "Neueste zuerst",
    openFirst:
      "Offene zuerst, die ältesten oben, inaktive danach; dann die neuesten",
    openFor: "{{age}} offen",
    idleFor: "{{idle}} ohne Ereignis",
    runningNote: " · Zeilen mit «Laufende Woche» zählen in keiner Zahl mit",
    firstOf: " · die ersten {{limit}} von {{count}}",
    noHits: "Keine Treffer für «{{q}}».",
    empty: "Keine Einträge im Zeitraum.",
    colPR: "PR",
    openPR: "PR in Bitbucket öffnen",
    colRepo: "Repo",
    colType: "Typ",
    colReview: "Bis Freigabe",
    colCycle: "Bis Merge",
    colToProd: "bis {{prod}}",
    colStatus: "Status",
    colAt: "Zeitpunkt",
    colApp: "Argo-CD-App",
    colStage: "Stage",
    colRevision: "GitOps-Commit",
    colNewImages: "Neue Version",
    colCountsAs: "Zählt als",
    colPipeline: "Pipeline",
    colRun: "Build",
    colKind: "Art",
    colCommit: "Commit",
    colDuration: "Dauer",
    colImages: "Gebaute Versionen",
    onProdAt: "Auf {{prod}} seit {{date}},",
    route: {
      compare: "erkannt am Vergleichs-Link im Release-Commit.",
      image: "erkannt an der Version, die ein Build gebaut hat.",
    },
    atMostTitle:
      "Höchstens: Vielleicht kam der PR schon mit einem früheren Deploy auf {{prod}}, das sich keinen PRs zuordnen lässt.",
    ...terms.de,
  },
  frameworks: {
    eyebrow: "Einordnung",
    title: "Welche Frameworks riptide abdeckt",
    lead: "Ohne Umfrage messbar: zwei der fünf DORA-Metriken, DX Core 4 teilweise.",
    row: {
      freq: "Releases nach {{prod}} pro Woche",
      lead: "Zeit vom ersten Commit bis {{prod}}",
      stages: "Fluss nach Abschnitten",
      dsucc: "Erfolgreiche Deploys",
      review: "Zeit bis zur Freigabe, innert 24 h gemerged",
      touch: "Freigabe ohne Kommentar",
      merged: "Gemergte PRs pro Woche",
      batch: "PR-Grösse",
      mix: "Arbeitsmix, ohne Jira-Key",
      build: "Build: Erfolg, Dauer, fehlgeschlagene Builds",
      costs: "Kosten der KI-Reviews",
      missingCfr: "fehlt: Change fail rate, Deployment rework rate",
      missingRecovery: "fehlt: Failed deployment recovery time",
      missingSurvey: "fehlt: Zufriedenheit, Cognitive load, Flow state",
    },
    cell: {
      dfThroughput: "Deployment frequency · Throughput",
      speedSecondary: "Speed, sekundär",
      velocityApprox: "Flow velocity, angenähert",
      cltThroughput: "Change lead time · Throughput",
      effWait: "Efficiency & flow (Wartezeit)",
      feedback: "Feedback loops",
      cltBreakdown: "Aufschlüsselung der Change lead time",
      effHandoffs: "Efficiency & flow (Übergaben, Wartezeit)",
      flowEffApprox: "Flow efficiency, angenähert",
      dsuccNot:
        "keine DORA-Metrik: zählt fehlgeschlagene Deploys, nicht die Change fail rate",
      diffsTeam:
        "Speed misst «Diffs pro Engineer»: riptide zählt pro Team, nie pro Person",
      activity: "Activity",
      flowVelocity: "Flow velocity",
      smallBatches: "Capability «Working in small batches»",
      feedbackReview: "Feedback loops (Code-Review)",
      flowTimeApprox: "Flow time, angenähert",
      collaboration: "Communication & collaboration (Review-Qualität)",
      impact: "Impact: Näherung für «Zeit für Neues», zählt PRs statt Zeit",
      distribution: "Flow distribution: Feature und Bugfix, ohne Debt und Risk",
      effectiveness: "Effectiveness: ein Treiber, nicht der DXI selbst",
      feedbackBuild: "Feedback loops (Build)",
      instability: "Instability",
      qualityCore: "Quality, Kernmetrik",
      throughput: "Throughput",
      qualitySecondary: "Quality, sekundär",
      dxiSurvey: "Effectiveness: DXI, nur per Umfrage",
      satisfactionSurvey: "Satisfaction & well-being, nur per Umfrage",
      surveyOnly: "nur per Umfrage",
    },
  },
} satisfies Catalog;
