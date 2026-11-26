import type { Catalog } from "./en";
import terms from "./terms.json";

// German UI catalog, shown for every de-* browser language unless the
// header's switch says otherwise. Swiss orthography: double s, never
// eszett. No gendered forms: generic masculine ("der Autor", "der
// Reviewer").
export const de = {
  lang: {
    code: "DE",
    switch: "Sprache: Deutsch. Auf Englisch wechseln",
  },
  nav: {
    label: "Bereiche",
    ueberblick: "Überblick",
    fluss: "Fluss",
    build: "Build",
    deploys: "Deploys",
    arbeitsmix: "Arbeitsmix",
    kosten: "Kosten",
    daten: "Daten",
    frameworks: "Frameworks",
  },
  common: {
    cw: "KW {{kw}}",
    hours: "{{n}} h",
    nOf: "{{n}} von {{total}}",
    nPRs_one: "{{count}} PR",
    nPRs_other: "{{count}} PRs",
    lever: "Hebel",
    typical: "{{unit}}, typischer Wert",
    slowest: "die langsamsten 10 %: über {{p90}}",
    lowSample: "wenig belastbar",
    noMergedPRs: "Im Zeitraum wurde kein PR gemerged.",
    shorterBetter: "kürzer ist besser",
    ownHistory: "nur mit dem eigenen Verlauf vergleichen",
    noDirection: "keine Richtung",
    withoutBots: "ohne Bots",
    withoutGitOps: "ohne GitOps-Repos",
    releases_one: "{{count}} Release",
    releases_other: "{{count}} Releases",
    resyncs_one: "{{count}} Sync ohne neue Images",
    resyncs_other: "{{count}} Syncs ohne neue Images",
    firstDeploys_one: "{{count}} Erstdeploy",
    firstDeploys_other: "{{count}} Erstdeploys",
    revisions_one: "{{count}} Revision",
    revisions_other: "{{count}} Revisionen",
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
      "Team {{team}}, {{product}} · KW {{from}}–{{to}} · {{start}} bis {{end}} · verglichen mit den {{weeks}} Wochen davor, {{prevStart}} bis {{prevEnd}}",
    scopeAlone:
      "Team {{team}}, {{product}} · KW {{from}}–{{to}} · {{start}} bis {{end}}",
    running:
      "Laufende Woche KW {{kw}} bis jetzt: {{releases}}, {{merged}}, {{runs}}. Zählt in keiner Zahl dieser Seite mit; nur die offenen PRs sind Stand jetzt.",
    runningReleases_one: "{{count}} Release",
    runningReleases_other: "{{count}} Releases",
    runningMerged_one: "{{count}} PR gemerged",
    runningMerged_other: "{{count}} PRs gemerged",
    runningRuns_one: "{{count}} Build",
    runningRuns_other: "{{count}} Builds",
    runningView: "Ansehen →",
    oneProduct: "Produkt {{product}}",
    allProductsLower: "alle Produkte",
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
    product: "Produkt",
    allProducts: "Alle Produkte",
    repos_one: "{{count}} Repo",
    repos_other: "{{count}} Repos",
    reposHead: "Repos",
    appsHead: "Argo-Apps",
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
    low_sample: "kein Vergleich: zu wenig Daten in einer der beiden Perioden",
    incomplete:
      "kein Vergleich: die Daten der Vorperiode sind nicht vollständig, oder Erstdeploys überwiegen",
    no_value: "kein Vergleich: ein Wert fehlt",
    not_reporting: "kein Vergleich: Argo CD meldet für das Team noch nicht",
    previous: "Vorperiode",
    now: "jetzt",
    before: "vorher {{v}}",
    undirected: "ohne Richtung",
  },
  strip: {
    title: "Gegen die Vorperiode",
    text: "Links schlechter, rechts besser; die Länge ist die Grösse der Veränderung, gekappt bei Halbierung oder Verdoppelung. Kennzahlen ohne Richtung fehlen.",
    aria: "Jede Kennzahl gegen die Vorperiode",
    reason: {
      low_sample: "zu wenig Daten",
      incomplete: "Vorperiode unvollständig",
      no_value: "kein Wert",
    },
    buildSuccess: "Erfolgsquote, {{kind}}",
  },
  why: {
    button: "Was heisst das?",
    means: "Was heisst das? · {{title}}",
    watch: "Genauer hinschauen, wenn",
    def: "Woher · Definition",
    basis: "Datenbasis",
    coverage: "Abdeckung",
    query: "Query:",
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
      weeks: "KW {{from}}–{{to}}",
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
          "{{source}} lieferte vom {{from}} bis {{to}} keine Daten, obwohl Builds liefen: Ereignisse gingen verloren. Die PR-Zahlen bleiben ohne Urteil.",
        silenceProduct:
          "{{source}} lieferte vom {{from}} bis {{to}} keine Daten, obwohl Builds liefen: Ereignisse gingen verloren. Die PR-Zahlen bleiben ohne Urteil, und mit ihnen Deploys und Builds: Sie kommen über Bitbucket zum Produkt.",
        silenceOngoing:
          "{{source}} liefert seit dem {{from}} keine Daten, obwohl Builds laufen: Ereignisse gehen verloren. Die PR-Zahlen bleiben ohne Urteil.",
        silenceOngoingProduct:
          "{{source}} liefert seit dem {{from}} keine Daten, obwohl Builds laufen: Ereignisse gehen verloren. Die PR-Zahlen bleiben ohne Urteil, und mit ihnen Deploys und Builds: Sie kommen über Bitbucket zum Produkt.",
        silent_end:
          "{{source}} lieferte seit dem {{from}} bis zum Ende des Zeitraums keine Daten mehr: Aussagen dazu ausgelassen.",
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
        "Dieses Produkt enthält nur Bibliotheken; sie werden nicht ausgerollt.",
      librariesOnlyTeam:
        "Alle PRs, die in diesen Wochen gemerged wurden, liegen in Bibliotheken; sie werden nicht ausgerollt.",
      quiet: "In diesen Wochen fällt nichts auf.",
      source: {
        bitbucket: "Bitbucket",
        pipeline: "Die CI",
        argocd: "Argo CD",
        noergler: "noergler",
      },
      focus: "Im Fokus",
      focusBy: "nach Gewicht der Befunde",
      scopeAll: "Team, alle Produkte",
      scopeProduct: "Produkt {{product}}",
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
          title: "Gemergte PRs warten lange auf {{prod}}",
          unit_one:
            "{{count}} gemergter PR noch nicht auf {{prod}}, seit {{n}} Tagen",
          unit_other:
            "{{count}} gemergte PRs noch nicht auf {{prod}}, der älteste seit {{n}} Tagen",
          threshold_one:
            "Abmachung: höchstens {{n}} Tag vom Merge bis {{prod}}.",
          threshold_other:
            "Abmachung: höchstens {{n}} Tage vom Merge bis {{prod}}.",
          rule: "Gezählt werden PRs von Repos, die ausgerollt werden, ab ihrer Ankunft auf dem Default-Branch; Bibliotheken nicht.",
        },
        slow_to_prod: {
          title: "Lange Wartezeit vom Merge bis {{prod}}",
          unit_one:
            "typisch {{n}} Tage vom Merge bis {{prod}}, aus {{count}} PR",
          unit_other:
            "typisch {{n}} Tage vom Merge bis {{prod}}, aus {{count}} PRs",
          threshold_one:
            "Abmachung: höchstens {{n}} Tag vom Merge bis {{prod}}. Beurteilt ab {{min}} PRs.",
          threshold_other:
            "Abmachung: höchstens {{n}} Tage vom Merge bis {{prod}}. Beurteilt ab {{min}} PRs.",
          rule: "Typischer Wert über die PRs, die im Zeitraum gemerged wurden und bis zu seinem Ende auf {{prod}} kamen.",
        },
        merge_pause: {
          title: "Kaum gemerged, obwohl freigegebene PRs warteten",
          unit_one:
            "KW {{week}}: {{count}} PR gemerged statt typisch {{typical}}, während {{n}} freigegebene PRs warteten ({{names}})",
          unit_other:
            "KW {{week}}: {{count}} PRs gemerged statt typisch {{typical}}, während {{n}} freigegebene PRs warteten ({{names}})",
          threshold:
            "Gemeldet, wenn eine Woche höchstens {{n}} der typischen Merges hat, während mindestens {{min}} freigegebene PRs bis zum Wochenende {{within}} oder länger warteten.",
          rule: "Verglichen mit der typischen Woche des Projekts. Warum nicht gemerged wurde, zeigen die Daten nicht.",
        },
        approved_waiting: {
          title: "Freigegeben, aber nicht gemerged",
          unit_one:
            "{{count}} freigegebener PR wartet auf den Merge, seit {{n}} Arbeitstagen",
          unit_other:
            "{{count}} freigegebene PRs warten auf den Merge, der älteste seit {{n}} Arbeitstagen",
          threshold_one:
            "Gemeldet ab {{min}} PRs pro Repo, die länger als {{n}} Arbeitstag warten.",
          threshold_other:
            "Gemeldet ab {{min}} PRs pro Repo, die länger als {{n}} Arbeitstage warten.",
          rule: "Freigegeben heisst: Eine Person, nicht der Autor, hat den PR freigegeben, und die Freigabe gilt noch.",
        },
        bot_loop: {
          title: "Bot-Branch verbraucht CI-Zeit mit fehlgeschlagenen Builds",
          unit: "{{n}} fehlgeschlagene Builds in Folge, {{ci}} CI-Zeit",
          threshold: "Gemeldet ab {{n}} fehlgeschlagenen Builds in Folge.",
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
          rule: "Ihr GitOps-Commit ist Bitbucket nicht bekannt. Für ihre Projekte ist die Abmachung «ein Release pro Sprint» nicht messbar.",
        },
        no_prod_stage: {
          title: "Argo CD meldet nichts nach {{prod}}",
          unit: "Argo CD meldet Deploys nach {{names}}, keines nach {{prod}}",
          rule: "Die Stage-Konfiguration oder die Benachrichtigung von Argo CD prüfen.",
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
          unit: "{{n}}-mal scheiterte das Deploy, bis wieder eines gelang; {{count}} Deploys im Zeitraum",
          threshold: "Gemeldet ab {{n}}-mal im Zeitraum.",
          rule: "Mehrere Versuche hintereinander zählen einmal.",
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
            "Gemeldet ab {{min}} PRs, die länger als {{n}} Arbeitstag warten, oder wenn das erste Review typisch so lange dauert.",
          threshold_other:
            "Gemeldet ab {{min}} PRs, die länger als {{n}} Arbeitstage warten, oder wenn das erste Review typisch so lange dauert.",
          rule: "PRs, die keine Entwürfe mehr sind und noch keine Reaktion einer Person haben. PRs von Bots nicht gezählt.",
        },
        trunk_failing: {
          title: "Builds auf dem Default-Branch lange fehlgeschlagen",
          unit_one:
            "{{days}} lang fehlgeschlagen, {{count}} fehlgeschlagener Build",
          unit_other:
            "{{days}} lang fehlgeschlagen, {{count}} fehlgeschlagene Builds",
          threshold_one:
            "Gemeldet ab {{min}} fehlgeschlagenen Builds über mehr als {{n}} Arbeitstag.",
          threshold_other:
            "Gemeldet ab {{min}} fehlgeschlagenen Builds über mehr als {{n}} Arbeitstage.",
          rule: "Vom ersten fehlgeschlagenen Build bis zum nächsten erfolgreichen. Release-Builds nicht gezählt.",
        },
        branch_failing: {
          title: "Builds eines Branches scheitern immer wieder",
          unit: "{{n}} fehlgeschlagene Builds in Folge",
          threshold:
            "Gemeldet ab {{n}} fehlgeschlagenen Builds in Folge über mindestens {{within}}.",
          rule: "Ein Feature- oder Bugfix-Branch einer Person. Branches von Bots nicht gezählt.",
        },
        flaky_builds: {
          title: "Unzuverlässige Builds",
          unit: "{{kind}}: {{n}} von {{of}} fehlgeschlagenen Builds liefen beim nächsten Versuch ohne Änderung durch",
          threshold:
            "Gemeldet ab {{min}} solchen Builds, wenn sie mindestens {{n}} aller fehlgeschlagenen Builds ausmachen.",
          rule: "Fehlgeschlagen, dann erfolgreich auf demselben Commit. Eine Untergrenze: Wiederholungen innerhalb eines Builds sieht riptide nicht.",
        },
        long_builds: {
          title: "Builds dauern lang",
          unit_one:
            "{{kind}}: typisch {{n}} Minuten, aus {{count}} erfolgreichen Build",
          unit_other:
            "{{kind}}: typisch {{n}} Minuten, aus {{count}} erfolgreichen Builds",
          threshold_one:
            "Gemeldet, wenn ein erfolgreicher Build typisch länger als {{n}} Minute dauert, aus mindestens {{min}} Builds.",
          threshold_other:
            "Gemeldet, wenn ein erfolgreicher Build typisch länger als {{n}} Minuten dauert, aus mindestens {{min}} Builds.",
          rule: "Typische Dauer der erfolgreichen Builds, pro Repo und Build-Art.",
        },
        release_failing: {
          title: "Release-Builds scheitern",
          unit: "{{count}} von {{of}} Release-Builds fehlgeschlagen ({{n}})",
          threshold:
            "Gemeldet ab {{min}} fehlgeschlagenen Release-Builds, wenn sie mindestens {{n}} aller Release-Builds ausmachen.",
          rule: "Fehlgeschlagene Release-Builds, pro Repo.",
        },
        builds_silent: {
          title: "CI meldet für ein Repo keine Builds mehr",
          unit_one:
            "{{n}} PRs gemerged, kein Build gemeldet; in den {{within}} davor {{count}} Build",
          unit_other:
            "{{n}} PRs gemerged, kein Build gemeldet; in den {{within}} davor {{count}} Builds",
          threshold_one:
            "Gemeldet ab {{min}} gemergten PRs im letzten {{n}} Arbeitstag ohne gemeldeten Build.",
          threshold_other:
            "Gemeldet ab {{min}} gemergten PRs in den letzten {{n}} Arbeitstagen ohne gemeldeten Build.",
          rule: "Ob dort gebaut wird, zeigen die Daten nicht: Vielleicht meldet die CI nur nicht mehr.",
        },
        big_prs: {
          title: "Viele grosse PRs",
          unit: "{{count}} von {{of}} PRs ändern mehr als {{lines}} Zeilen ({{n}})",
          threshold:
            "Gemeldet, wenn mindestens {{n}} der PRs mehr als {{min}} Zeilen ändern.",
          rule: "Anteil an den gemergten PRs mit bekannter Grösse.",
        },
        fast_approval: {
          title: "Grosse PRs schnell freigegeben",
          unit_one:
            "{{n}} grosser PR innert {{within}} freigegeben, ohne Kommentar davor",
          unit_other:
            "{{n}} grosse PRs innert {{within}} freigegeben, ohne Kommentar davor",
          threshold:
            "Gemeldet ab {{n}} PRs über {{min}} Zeilen, innert {{within}} freigegeben.",
          rule: "Ohne Kommentar einer Person davor. Die Daten zeigen die Zeit, nicht wie geprüft wurde.",
        },
        unapproved: {
          title: "Ohne Freigabe gemerged",
          unit: "{{count}} von {{of}} gemergten PRs ohne Freigabe einer Person ({{n}})",
          threshold:
            "Gemeldet ab {{min}} gemergten PRs pro Repo, wenn mindestens {{n}} ohne Freigabe sind.",
          rule: "Aufräum-PRs und PRs von Bots nicht gezählt.",
        },
        late_decline: {
          title: "PRs nach langer Arbeit abgelehnt",
          unit_one: "{{n}} PR nach mehr als {{within}} abgelehnt ({{names}})",
          unit_other:
            "{{n}} PRs nach mehr als {{within}} abgelehnt ({{names}})",
          threshold: "Gemeldet ab {{n}} solchen PRs im Team.",
          rule: "Arbeit, die nicht ankam. Nicht gezählt, wenn ein späterer PR vom selben Branch sie weiterführt.",
        },
        branch_life: {
          title: "Branches leben lange, bevor der PR bereit ist",
          unit_one:
            "typisch {{n}} Arbeitstage vom ersten Commit, bis der PR bereit ist, aus {{count}} PR",
          unit_other:
            "typisch {{n}} Arbeitstage vom ersten Commit, bis der PR bereit ist, aus {{count}} PRs",
          threshold_one:
            "Gemeldet ab typisch mehr als {{n}} Arbeitstag, aus mindestens {{min}} PRs.",
          threshold_other:
            "Gemeldet ab typisch mehr als {{n}} Arbeitstagen, aus mindestens {{min}} PRs.",
          rule: "Eine Untergrenze: Ein Rebase setzt den ersten Commit neu.",
        },
        branch_open: {
          title: "Branches ohne PR, lange in Arbeit",
          unit_one:
            "{{count}} Branch ohne PR in Arbeit, seit {{n}} Tagen ({{names}})",
          unit_other:
            "{{count}} Branches ohne PR in Arbeit, der älteste seit {{n}} Tagen ({{names}})",
          threshold_one:
            "Gemeldet ab einem Branch ohne PR, zuerst vor mehr als {{n}} Tag gepusht und zuletzt vor höchstens {{within}}.",
          threshold_other:
            "Gemeldet ab einem Branch ohne PR, zuerst vor mehr als {{n}} Tagen gepusht und zuletzt vor höchstens {{within}}.",
          rule: "Branches, die Ziel von PRs sind, und Branches von Bots nicht gezählt.",
        },
        branch_silent: {
          title: "Branches ohne PR, lange still",
          unit_one: "{{count}} Branch ohne PR nicht mehr gepusht ({{names}})",
          unit_other:
            "{{count}} Branches ohne PR nicht mehr gepusht ({{names}})",
          threshold_one:
            "Gemeldet ab {{min}} Branches ohne PR pro Repo, seit mehr als {{n}} Tag nicht gepusht.",
          threshold_other:
            "Gemeldet ab {{min}} Branches ohne PR pro Repo, seit mehr als {{n}} Tagen nicht gepusht.",
          rule: "Ob sie noch existieren, meldet Bitbucket nicht.",
        },
        unmeasurable: {
          title: "{{was}}: nicht mehr messbar",
          unit: "",
          rule: "Vor einer Woche hat riptide es hier festgestellt; jetzt reichen die Daten nicht, um es zu beurteilen. Das heisst nicht, dass es gelöst ist.",
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
        agent_wait: "Agent-Wartezeit",
        resolved: "erledigt",
        question: "Frage",
        state: "Befund",
      },
      title: {
        data_gap: "{{source}} meldet nichts",
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
          "Die Quelle meldet seit vor dem Periodenende nichts; ihre Zahlen sagen nichts über die Arbeit.",
        pr_jam:
          "Vier oder mehr PRs seit einer Woche oder länger offen, Entwürfe nicht gezählt. Warum sie liegen, steht nicht in den Daten: ein Freeze, eine Abhängigkeit, Ferien oder niemand fühlt sich zuständig.",
        slower_build:
          "Der typische erfolgreiche Build dauert anderthalbmal so lang wie in der Vorperiode oder länger: langsamer als das Repo selbst, nicht als andere. Was im Build langsamer wurde, steht nicht in den Daten.",
        failing_more:
          "Scheitert viel öfter als in der Vorperiode. Ob Tests, Infrastruktur oder echte Fehler, steht nicht in den Daten.",
        changeGood:
          "Über der Schwelle für eine wichtige Veränderung gegenüber der Vorperiode.",
        changeBad:
          "Über der Schwelle für eine wichtige Veränderung gegenüber der Vorperiode. Warum, steht nicht in den Daten.",
        agent_wait:
          "{{share}} der typischen Build-Zeit oder mehr, und mindestens {{minutes}} Minuten, ist Warten auf einen Agent: die Kapazität der Plattform, nicht der Build.",
        resolved:
          "Die letzte Einschätzung hat es aufgeworfen; jetzt ist der Wert zurück auf dem Stand davor oder besser.",
        resolvedState:
          "Vor einer Woche hat riptide es hier festgestellt; jetzt liegt jede damals genannte Einheit wieder klar unter der Grenze.",
      },
      factsOf: "Fakten zu {{repo}} ansehen →",
      driver: "Treiber: {{repo}} trägt {{share}} der Zunahme",
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
          "klären, worauf sie warten: ein Freeze, eine Abhängigkeit, oder niemand fühlt sich zuständig",
        inspect_build_log:
          "im Build-Log der langsamsten Builds nachsehen, welche Stufe die Zeit braucht",
        inspect_failures:
          "die fehlgeschlagenen Builds ansehen: jedes Mal derselbe Fehler oder ein anderer?",
        platform_agents:
          "mit dem Plattform-Team die Agent-Kapazität ansprechen",
        find_what_changed:
          "erst klären, was sich geändert hat, dann etwas vereinbaren",
        name_what_helped: "benennen, was geholfen hat, damit es bleibt",
        check_source: "die Anbindung der Quelle prüfen",
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
        silent_days: "ohne Meldung",
        open_long: "PRs ≥ 7 Tage offen",
        open: "offene PRs",
        age_p50: "typisches Alter",
        failure_rate: "Builds fehlgeschlagen",
        wasted: "CI-Zeit in fehlgeschlagenen Builds",
        runs: "Builds",
        build_p50: "typischer erfolgreicher Build",
        build_wait_p50: "Warten auf einen Agent",
        build_work_p50: "Bauen",
        deploy_per_week: "Releases pro Woche",
        deploy_success: "Deploys erfolgreich",
        lead_time_p50: "Lead Time",
        review_p50: "bereit bis Approval",
        within_day: "innert eines Tages gemerged",
        jammed_repos: "Repos im Stau",
        build_success: "Builds erfolgreich",
        merged_per_week: "PRs gemerged pro Woche",
        batch_size: "geänderte Zeilen pro PR",
        wip_weeks: "Arbeit offen",
        feature_share: "Feature-PRs",
        untracked: "PRs ohne Jira-Key",
        wasted_share: "Review-Aufwand für verworfene PRs",
        cost_per_kloc: "Review-Aufwand pro 1000 Zeilen",
        open_prs: "offene PRs",
        first_touch: "Approvals ohne vorherigen Kommentar",
        state_units: "genannt",
        state_judged: "beurteilt",
        state_threshold: "Schwelle des Teams",
        state_no_release: "Tage seit dem letzten Release",
        state_release_due: "Tage seit dem letzten Release",
        state_not_on_prod: "Tage, die der älteste PR wartet",
        state_slow_to_prod: "typische Tage bis {{prod}}",
        state_merge_pause: "freigegebene PRs, die warteten",
        state_approved_waiting: "Arbeitstage, die der älteste PR wartet",
        state_bot_loop: "fehlgeschlagene Builds in Folge",
        state_untraced: "PRs nicht zuordenbar",
        state_unattributed: "Releases ohne Projekt",
        state_no_prod_stage: "gemeldete Stages",
        state_deploy_failing: "Arbeitstage ohne erfolgreiches Deploy",
        state_deploy_failures: "Folgen fehlgeschlagener Deploys",
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
        state_branch_silent: "Branches still",
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
      "Argo CD meldet erst seit {{date}}: die Wochen davor sind leer, die Rate gilt ab dann.",
    sinceLow: " Weniger als eine Woche Daten: wenig belastbar.",
    throughput: "Throughput und Instability",
    freq: {
      label: "Deploy-Frequenz",
      dora: "DORA · Deployment frequency · Throughput",
      question: "Wie oft geht neuer Code nach {{prod}}?",
      unit: "Releases pro Woche, im Schnitt",
      note_one: "{{count}} Release im Zeitraum",
      note_other: "{{count}} Releases im Zeitraum",
      noDeploys: "keine Deploys im Zeitraum",
      notReporting: "Argo CD meldet für das Team noch nicht",
      direction: "öfter ist besser",
      directionWhy: "kleinere Releases, weniger Risiko pro Release",
      firstSeen_one:
        "{{count, num}} Erstdeploy aus den ersten {{days}} Tagen der Argo-CD-Daten: ob mit neuer Version, ist unbekannt; nicht gezählt",
      firstSeen_other:
        "{{count, num}} Erstdeploys aus den ersten {{days}} Tagen der Argo-CD-Daten: ob mit neuer Version, ist unbekannt; nicht gezählt",
      unattributed_one: "{{count, num}} Deploy keinem Produkt zuordenbar",
      unattributed_other: "{{count, num}} Deploys keinem Produkt zuordenbar",
      infra_one: "{{count, num}} Deploy von Infrastruktur-Apps nicht gezählt",
      infra_other:
        "{{count, num}} Deploys von Infrastruktur-Apps nicht gezählt",
      means:
        "Wie oft neuer Code bei den Nutzern ankommt. Häufige kleine Releases tragen wenig Risiko je Release und bringen Feedback früh.",
      watch:
        "die Wochen stark schwanken oder Releases ausbleiben: dann sammeln sich Änderungen zu grossen Releases.",
      lever:
        "kleinere Änderungen, automatisierte Deploys, kein fester Release-Termin für PROD.",
      def: "Erfolgreiche Deploys auf {{prod}} mit neuer Version: Die Version unterscheidet sich vom vorherigen Deploy derselben Argo-App, oder es ist der erste Deploy einer App, die live geht. Einmal pro GitOps-Revision gezählt, geteilt durch die Wochen im Zeitraum, in denen Argo CD meldete. Deploys von Infrastruktur-Apps, deren Deploys nie einem Produkt des Teams zuzuordnen waren, zählen nicht.",
      basis:
        "{{releases}}, {{resyncs}} und {{firstSeen}} aus {{syncs}}. Aktualisiert ein Release mehrere Apps über dieselbe GitOps-Revision, zählt es einmal.",
      basisFirst_one: " Davon {{count}} Release als erster Deploy einer App.",
      basisFirst_other:
        " Davon {{count}} Releases als erster Deploy einer App.",
      filterImages: "images ≠ vorheriger Deploy",
      filterRevision: "pro revision",
      coverageFirst:
        "In den ersten {{days}} Tagen der Argo-CD-Daten hat der erste Deploy einer App keinen Vorgänger: Ob er eine neue Version brachte, ist unbekannt, er zählt nicht als Release. Danach zählt der erste Deploy einer App als Release: Die App geht live.",
      coverageAll:
        "Jeder Deploy ist vergleichbar: Der erste Deploy einer App nach den ersten {{days}} Tagen der Argo-CD-Daten zählt als Release.",
      coverageInfra_one:
        " Nicht gezählt: {{count, num}} Deploy von Infrastruktur-Apps, die keinem Produkt des Teams gehören ({{apps}}).",
      coverageInfra_other:
        " Nicht gezählt: {{count, num}} Deploys von Infrastruktur-Apps, die keinem Produkt des Teams gehören ({{apps}}).",
      coverageUnattributed_one:
        " {{count, num}} Deploy ist keinem Produkt zuordenbar: Weder sein GitOps-Commit noch ein anderer Deploy seiner App nennt eines. In der Teamansicht zählt er, in keiner Produktansicht.",
      coverageUnattributed_other:
        " {{count, num}} Deploys sind keinem Produkt zuordenbar: Weder ihr GitOps-Commit noch ein anderer Deploy ihrer App nennt eines. In der Teamansicht zählen sie, in keiner Produktansicht.",
    },
    lead: {
      label: "Lead Time bis {{prod}}",
      dora: "DORA · Change lead time · Throughput",
      question: "Wie lange braucht ein PR vom ersten Commit bis {{prod}}?",
      noHuman: "kein gemergter PR bis {{prod}} verfolgt",
      slowest_one: "die langsamsten 10 %: über {{p90}} · aus {{count}} PR",
      slowest_other: "die langsamsten 10 %: über {{p90}} · aus {{count}} PRs",
      directionWhy: "schnelleres Feedback von echten Nutzern",
      lowSample: "Weniger als 10 PRs: wenig belastbar",
      means:
        "Die Zeit, in der eine fertige Änderung noch keinem Nutzer hilft. Lang heisst: Feedback kommt spät, Fehler lassen sich schwer einer Änderung zuordnen, Releases werden gross.",
      watch:
        "die langsamsten 10 % weit über dem typischen Wert liegen: dann bleiben einzelne Änderungen hängen, oft hinter einem Release-Termin.",
      lever:
        "Wo die Zeit liegt, zeigt der Fluss darunter; dort liegt der Hebel, nicht in den kurzen Abschnitten.",
      def: "Pro PR vom ersten Commit bis zum ersten Deploy auf {{prod}}, der ihn enthält. Die PRs, die im Zeitraum gemerged wurden und bis zu seinem Ende auf {{prod}} waren. PRs von Bots und von Bibliotheken zählen nicht.",
      basis_one:
        "Aus {{count}} PR, gemerged im Zeitraum und bis zu seinem Ende auf {{prod}}.",
      basis_other:
        "Aus {{count}} PRs, gemerged im Zeitraum und bis zu seinem Ende auf {{prod}}.",
      filterBots: "ohne Bots",
      coverage_one:
        "{{traced, num}} von {{count, num}} gemergten PR bis {{prod}} verfolgt: über den Compare-Link im Release-Commit oder die Version, die ein Build gebaut hat. Ein Release ohne beides fehlt hier.",
      coverage_other:
        "{{traced, num}} von {{count, num}} gemergten PRs bis {{prod}} verfolgt: über den Compare-Link im Release-Commit oder die Version, die ein Build gebaut hat. Ein Release ohne beides fehlt hier.",
      traced_one:
        "{{traced, num}} von {{count, num}} gemergten PR bis {{prod}} verfolgt",
      traced_other:
        "{{traced, num}} von {{count, num}} gemergten PRs bis {{prod}} verfolgt",
      notYet_one: "{{count, num}} noch nicht auf {{prod}}",
      notYet_other: "{{count, num}} noch nicht auf {{prod}}",
      unmeasured_one:
        "{{count, num}} davon ohne bekannten Start, nicht in der Zeit",
      unmeasured_other:
        "{{count, num}} davon ohne bekannten Start, nicht in der Zeit",
    },
    dsucc: {
      label: "Deploy-Erfolg",
      dora: "keine DORA-Metrik",
      question: "Klappt das Ausrollen selbst?",
      unit: "der Deploys",
      note_one: "{{failed}} von {{count}} Deploy fehlgeschlagen",
      note_other: "{{failed}} von {{count}} Deploys fehlgeschlagen",
      direction: "höher ist besser",
      means:
        "Ob das Ausrollen selbst klappt: ein abgebrochener oder fehlgeschlagener Deploy heisst, jemand muss eingreifen.",
      watch:
        "der Wert sinkt: dann scheitern Deploys an Konfiguration, Ressourcen oder Abhängigkeiten.",
      lever:
        "fehlgeschlagene Deploys unter «Daten» ansehen; meist ist es dieselbe App oder dieselbe Ursache.",
      def: "Anteil der Deploys auf {{prod}} mit operation_phase Succeeded an allen abgeschlossenen Deploys; Failed und Error zählen als fehlgeschlagen. Keine DORA-Metrik: die Change fail rate zählt Deploys, nach denen jemand eingreifen muss, auch wenn der Deploy gelang.",
      basis:
        "{{finished}} abgeschlossene Deploys, davon {{failed}} fehlgeschlagen (Failed oder Error).",
      coverage:
        "Nur Deploys, die Argo CD gemeldet hat; Deploys von Infrastruktur-Apps zählen nicht.",
    },
    instability: {
      label: "Instability",
      dora: "DORA · Change fail rate · Deployment rework rate · Failed deployment recovery time",
      question:
        "Wie oft macht ein Deployment Ärger, und wie schnell ist es behoben?",
      value: "nicht gemessen",
      text: "Dafür fehlt eine Incident-Quelle.",
    },
  },
  flow: {
    stage: {
      coding: "Coding",
      codingDesc: "erster Commit → PR bereit",
      review: "Review",
      reviewDesc: "PR bereit → Approval",
      merge: "Merge",
      mergeDesc: "Approval → gemerged",
      toPre: "Bis {{pre}}",
      toPreDesc: "gemerged → auf {{pre}}",
      toProdPre: "Von {{pre}} bis {{prod}}",
      toProd: "Ab Merge bis {{prod}}",
      toProdDescPre: "auf {{pre}} (ohne: gemerged) → auf {{prod}}",
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
      "Pro gemergtem PR in Repos, die ausgerollt werden, typische Dauer je Abschnitt; die Lead Time oben zählt dieselben PRs bis {{prod}}.",
    leadTo: "Lead Time bis {{env}}:",
    typicalSlowest: "typischer Wert · die langsamsten 10 %: über {{p90}}",
    shareAria: "Anteil an der Zeit bis {{prod}}: {{parts}}",
    ofTime: "· {{share}} der Zeit",
    hot: "Der Hebel liegt bei «{{name}}»",
    hotRest: ": {{share}} der Zeit bis {{prod}}.",
    noProd:
      "In diesem Zeitraum wurde keine Änderung bis {{prod}} verfolgt: Anteile gibt es nur über die ganze Strecke.",
    chainNote_one:
      "Die letzten zwei Abschnitte erreichen nur PRs, die einem Deploy zugeordnet sind: {{traced, num}} von {{count, num}} gemergten PR bis {{prod}} verfolgt.",
    chainNote_other:
      "Die letzten zwei Abschnitte erreichen nur PRs, die einem Deploy zugeordnet sind: {{traced, num}} von {{count, num}} gemergten PRs bis {{prod}} verfolgt.",
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
      "{{count, num}} PR in Bibliotheken, werden nicht ausgerollt: in keiner Zählung bis {{prod}}.",
    library_other:
      "{{count, num}} PRs in Bibliotheken, werden nicht ausgerollt: in keiner Zählung bis {{prod}}.",
    perRepo_one: "Pro Repo ({{count}} Repo)",
    perRepo_other: "Pro Repo ({{count}} Repos)",
    colRepo: "Repo",
    colMerged: "Gemerged",
    colOnProd: "Auf {{prod}}",
    colNotYet: "Noch nicht auf {{prod}}",
    colNotPlaceable: "Nicht zuordenbar",
    colLibrary: "Bibliothek",
    oldestSince: "ältester gemerged am {{date}}",
    ridge: {
      title: "Wie die Zeit in jedem Abschnitt verteilt ist",
      sub: "Dieselben PRs wie oben, Zeitachse logarithmisch. Strich: typischer Wert, gestrichelt: ab da die langsamsten 10 %.",
      caption: "Wartezeit je Abschnitt",
      colStage: "Abschnitt",
      few: "unter {{min}} PRs: einzelne PRs statt Kurve",
      none: "keine PRs",
      tipN: "PRs",
      tipTypical: "typischer Wert",
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
    eyebrow: "Vom Commit nach {{prod}} · nur App-Repos",
    title: "Wo wartet eine Änderung?",
    lead: "Die Zeit bis zum Nutzer, aufgeteilt in Abschnitte.",
    fwLeadTime: "Change lead time, aufgeschlüsselt",
    reviewTitle: "Review: schnell, aber wie gründlich?",
    review: {
      label: "Review-Zeit",
      question: "Wie lange wartet ein PR auf das erste Approval?",
      note_one: "die langsamsten 10 %: über {{p90}} · {{count}} PR",
      note_other: "die langsamsten 10 %: über {{p90}} · {{count}} PRs",
      means:
        "Wie lange ein fertiger PR auf das erste Approval wartet. In dieser Zeit liegt die Arbeit still, und der Autor beginnt oft schon etwas Neues.",
      watch:
        "die langsamsten 10 % weit über dem typischen Wert liegen: einzelne PRs bleiben liegen, oft grosse oder solche ohne klaren Reviewer.",
      lever:
        "Review-Zeiten im Team vereinbaren, PRs klein halten, Reviewer gleich beim Öffnen nennen.",
      def: "Von PR bereit (opened, bei Drafts ready_for_review) bis zum ersten Approval eines menschlichen Reviewers.",
      basis_one: "{{count}} PR mit Approval.",
      basis_other: "{{count}} PRs mit Approval.",
      filterReviewer: "Reviewer ≠ PR-Ersteller",
      coverage: "Vollständig für Bitbucket-Events.",
    },
    touch: {
      label: "Approval ohne Kommentar",
      question: "Wie oft ist das Approval die erste Reaktion des Reviewers?",
      directionWhy: "Gesprächsanlass, kein Ziel",
      means:
        "Wie oft das Approval die erste Reaktion des Reviewers ist. Hoch heisst: Review ohne sichtbare Diskussion in Bitbucket.",
      watch:
        "der Wert zusammen mit sehr kurzen Review-Zeiten steigt: dann wird vielleicht durchgewinkt.",
      lever:
        "keine Zielvorgabe. Im Team besprechen, ob grosse Änderungen anderswo diskutiert werden.",
      def: "Anteil der approvten PRs, deren erster Reviewer-Kontakt das Approval selbst ist.",
      basis: "{{n}} von {{total}} approvten PRs.",
      filterComments: "ohne Bot-Kommentare",
      coverage: "Mündliche Reviews und Pairing sieht Riptide nicht.",
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
        "viele PRs über drei Tage offen sind: das sind meist grosse PRs oder solche, die auf jemanden warten.",
      lever: "PRs kleiner schneiden, Review früh anfragen.",
      def: "pr:merged minus pr:opened pro PR, Schlüssel (repo_full_name, pr_id). Die PR-Cycle-Time ist typischerweise {{p50}}, die langsamsten 10 % über {{p90}}.",
      basis: "{{count}} von {{n}} menschlichen PRs, gemerged im Zeitraum.",
      filterMerged: "pr:merged im Zeitraum",
      coverage:
        "PRs, die vor Datenbeginn geöffnet wurden, haben kein pr:opened und fehlen. Beim Öffnen gesehen: {{seen, num}} von {{total, num}}, die gemerged wurden.",
    },
    histHead: "PRs nach Dauer vom Öffnen bis zum Merge",
    histLine: " · die Linie markiert 24 h",
    histAria: "PR-Cycle-Time: {{parts}}",
  },
  throughput: {
    head: "Flow: wie viel wird fertig, in welchen Portionen",
    cardTitle: "PR Throughput und Batch Size pro Woche",
    scope: "Nur PRs von Menschen: ohne Stage-Promotions, ohne Bots.",
    merged: {
      label: "PR Throughput",
      framework: "Flow · Flow velocity",
      question: "Wie viele PRs merged das Team pro Woche?",
      unit: "PRs pro Woche",
      note_one: "{{count, num}} PR im Zeitraum gemerged",
      note_other: "{{count, num}} PRs im Zeitraum gemerged",
      housekeeping_one: "{{count, num}} Aufräum-PR nicht gezählt",
      housekeeping_other: "{{count, num}} Aufräum-PRs nicht gezählt",
      housekeepingTitle:
        "Derselbe Titel in {{repos}} oder mehr Repos innert {{minutes}} Minuten eröffnet, ohne Approval einer Person und ohne Ticket-Key: Pflege mit einem Werkzeug über alle Repos, keine Arbeit am Produkt.",
      umbrella_one: "{{count, num}} Merge eines Sammel-Branches nicht gezählt",
      umbrella_other: "{{count, num}} Merges von Sammel-Branches nicht gezählt",
      umbrellaTitle:
        "Der PR brachte nur die Arbeit anderer PRs auf den Hauptbranch: Sie wurden in seinen Branch gemerged oder mit ihm. Diese PRs zählen, er selbst nicht.",
      twice_one: "{{count, num}} PR doppelt, nicht gezählt",
      twice_other: "{{count, num}} PRs doppelt, nicht gezählt",
      twiceTitle:
        "Derselbe Branch kam schon mit einem anderen PR in einen Sammel-Branch und zählt dort. Als der Sammel-Branch gemerged wurde, galt dieser PR mit als gemerged: dieselbe Arbeit, einmal gezählt.",
      since:
        "Bitbucket liefert erst seit dem {{date}} Daten: Die Wochen davor sind leer, die Rate gilt ab dann.",
      direction: "mehr ist besser",
      chart: "PRs gemerged pro Woche, nach Branch-Präfix",
    },
    batch: {
      label: "Batch Size",
      framework: "DORA-Capability · Working in small batches",
      question: "Wie gross ist ein typischer PR?",
      unit: "geänderte Zeilen pro PR, typischer Wert",
      note: "grösste 10 % ab {{p90}} Zeilen · typisch {{files}} Dateien · Grösse bekannt bei {{n}} von {{total}}",
      none: "Keine PR-Grössen im Zeitraum.",
      direction: "kleiner ist besser",
      chart: "Typische PR-Grösse pro Woche, in geänderten Zeilen",
      series: "Geänderte Zeilen, typisch",
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
      unit: "typischerweise {{p50}} alt",
      note: "die ältesten 10 %: über {{p90}}",
      directionWhy: "mehr offene PRs kann auch mehr Arbeit im Gang heissen",
    },
    longWaitOn: "Die lang offenen warten auf:",
    inactive_one:
      "{{count}} PR inaktiv: seit {{days}} Tagen kein Event, in keiner Zahl oben. Ein verlorenes Merge-Event sieht gleich aus.",
    inactive_other:
      "{{count}} PRs inaktiv: seit {{days}} Tagen kein Event, in keiner Zahl oben. Ein verlorenes Merge-Event sieht gleich aus.",
    unknown_one:
      "{{count, num}} PR mit unbekanntem Zustand: Zuletzt gesehen vor einem Ausfall, in dem Bitbucket keine Daten lieferte; ob er seither gemerged oder geschlossen wurde, ist unbekannt. In keiner Zahl oben.",
    unknown_other:
      "{{count, num}} PRs mit unbekanntem Zustand: Zuletzt gesehen vor einem Ausfall, in dem Bitbucket keine Daten lieferte; ob sie seither gemerged oder geschlossen wurden, ist unbekannt. In keiner Zahl oben.",
    viewInactive: "Ansehen →",
    jam: "Stau",
    colRepo: "Repo",
    colOpen: "Offen",
    colLong: "≥ {{days}} T.",
    colMedian: "Median-Alter",
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
        "mehrere lang offene PRs im selben Repo auf ein erstes Review warten: dort fehlt ein Reviewer, oder die PRs sind zu gross.",
      lever:
        "Reviewer beim Öffnen nennen, alte PRs im Daily ansprechen, PRs schliessen, die niemand mehr braucht.",
      def: "PRs, die jetzt offen sind, auch in der laufenden Woche geöffnete; verglichen mit den am Ende der Vorperiode offenen: geöffnet davor, weder gemerged, abgelehnt noch gelöscht. Alter ab dem letzten Wechsel auf bereit für Review, sonst ab dem Erstelldatum: Zeit als Entwurf zählt nicht, ein Entwurf hat kein Alter. Lang offen ab {{days}} Tagen, Stau ab {{min}} lang offenen im selben Repo. Ein Entwurf ist nie lang offen und nie vergessen: er wartet auf seinen Autor. Pro Repo das Median-Alter, nie der älteste: einer allein bestimmt ihn.",
      basis_one: "{{count}} offener PR, Stand {{at}}.",
      basis_other: "{{count}} offene PRs, Stand {{at}}.",
      coverage:
        "Ein PR, dessen Merge oder Schliessen Bitbucket nie gemeldet hat, bleibt offen, bis er nach {{inactive}} Tagen ohne Event als inaktiv zählt; allein macht er nie einen Stau. Kam sein letztes Event vor einem Ausfall, in dem Bitbucket keine Daten lieferte, obwohl Builds liefen, und seither keines mehr, ist sein Zustand unbekannt: Er zählt nicht als offen. Ein PR ohne jedes Event seit Datenbeginn fehlt.",
    },
  },
  deploys: {
    eyebrow: "Ausrollen",
    title: "Wie gleichmässig kommt Code nach {{prod}}?",
    fwSpeed: "Speed, sekundär",
    cardTitle: "Releases auf {{prod}} pro Woche",
    cardSub:
      "Nur Releases, Deploys mit neuer Version; die Linie ist die typische Woche des Teams",
    release: "Release",
    releases: "Releases",
    typical: "typische Woche: {{n}}",
    aria: "Releases auf {{prod}} pro Woche: {{parts}}",
    note: "{{resyncs}} zählen nicht. {{firstSeen}} aus den ersten {{days}} Tagen der Argo-CD-Daten zählen auch nicht: Ob sie eine neue Version brachten, ist offen. Die typische Woche: der mittlere Wert der ganzen Wochen, in denen Argo CD meldete.",
  },
  calendar: {
    cardTitle: "Releases auf {{prod}} pro Tag",
    cardSub:
      "Jedes Release am Tag seines ersten Deployments; rechts je Wochentag die Releases pro ganzen Tag mit Daten",
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
    tableCaption: "Releases auf {{prod}} pro Tag; {{noData}}",
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
    lead: "Nach Branch-Präfix, als Anteil an PRs, nicht an Zeit.",
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
    title: "Fehlerquote pro Repo und Woche",
    sub: "Eine rote Zeile heisst: dieses Repo. Eine rote Spalte heisst: alle gleichzeitig, eher die Infrastruktur.",
    aria: "Fehlerquote pro Repo und Woche, {{kind}}",
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
      release: "build_type release oder hotfix",
      main: "master oder main, kein Release",
      feature:
        "feature/, feat/, bugfix/, fix/, hotfix/, chore/, refactor/ und PR-Builds",
      other:
        "docs/ und doc/ (oft ohne Build), release/… ohne Release, Nightlies, eigene Namensschemas",
      unknown: "vor der ersten build_type-Meldung",
    },
    prefixes: "Häufigste Präfixe",
    noBranch: "ohne Branch im Pipeline-Namen",
    featureMissingTitle: "Feature- und Bugfix-Builds: kein Build erkannt",
    featureMissing_one:
      "Erkannt werden Branches mit feature/, feat/, bugfix/, fix/, hotfix/, chore/, refactor/ und PR-Builds. {{count}} Build auf einem Branch ohne bekanntes Präfix steht unter «Übrige Branches» (release/, docs/, doc/, Nightlies und Bots nicht mitgezählt), vermutlich ein Feature-Build des Teams.",
    featureMissing_other:
      "Erkannt werden Branches mit feature/, feat/, bugfix/, fix/, hotfix/, chore/, refactor/ und PR-Builds. {{count}} Builds auf Branches ohne bekanntes Präfix stehen unter «Übrige Branches» (release/, docs/, doc/, Nightlies und Bots nicht mitgezählt), vermutlich die Feature-Builds des Teams.",
    sparkAria: "Erfolgsquote pro Woche, {{kind}}: {{parts}}",
    successful: "erfolgreich",
    typicalDuration: "typischer erfolgreicher Build, alle Repos",
    repos:
      "Von {{compared}} Repos sind {{slower}} langsamer, {{failing}} fehleranfälliger geworden als in der Vorperiode",
    reposNone: "Repos: kein Vergleich mit der Vorperiode",
    redRuns: "in fehlgeschlagenen Builds, von {{h}} CI-Zeit",
    waitingTitle:
      "Typischer Wert über {{n}} der {{total}} Builds, die melden, wann sie einen Agent bekamen",
    waiting:
      "davon typischerweise {{wait}} Warten auf einen Agent, {{build}} Bauen",
    scale: "KW {{kw}} · Erfolg, Skala {{low}}–100 %",
    runs_one: "{{count}} Build",
    runs_other: "{{count}} Builds",
    noVerdict: " + {{n}} ohne Ergebnis",
    outliers: " · {{n}} Ausreisser ({{h}}) nicht in Dauer und CI-Zeit",
    flaky_one:
      "{{count}} fehlgeschlagener Build flaky: danach erfolgreich auf demselben Commit ({{share}} der fehlgeschlagenen, {{h}})",
    flaky_other:
      "{{count}} fehlgeschlagene Builds flaky: danach erfolgreich auf demselben Commit ({{share}} der fehlgeschlagenen, {{h}})",
    flakyNone:
      "Kein fehlgeschlagener Build war danach auf demselben Commit erfolgreich",
    flakyNoRed: "Kein fehlgeschlagener Build, also auch kein flaky",
    unjudged_one: " · {{count}} Build ohne Commit nicht beurteilt",
    unjudged_other: " · {{count}} Builds ohne Commit nicht beurteilt",
    flakyRate: "Flaky-Quote {{rate}} der Builds mit Commit",
    viewFlaky: "Flaky-Builds ansehen →",
    flakyTitle:
      "Flaky: Ein Build schlägt fehl, ein späterer Build derselben Pipeline auf demselben Commit ist erfolgreich. Untergrenze: Wiederholungen innerhalb eines Builds, nur einmal gebaute Commits und Reruns nach dem Periodenende fehlen. Ob Tests oder Infrastruktur scheiterten, steht nicht in den Daten.",
    repoFlaky: "{{n}} flaky",
    repoFlakyTitle:
      "{{h}} davon in Builds, die ein späterer Build auf demselben Commit erfolgreich machte",
    failingMore: "Fehleranfälliger geworden",
    failingMoreText: "Vorperiode {{prev}} → jetzt {{rate}} fehlgeschlagen",
    slow: "Langsamer geworden",
    slowText: "Vorperiode {{prev}} → jetzt {{p50}} ({{factor}}×)",
    reliable: "Verlässlicher geworden",
    faster: "Schneller geworden",
    fasterText: "Vorperiode {{prev}} → jetzt {{p50}}",
    fine: "Unauffällig",
    noCompare: "Kein Vergleich",
    noCompareNoValue:
      "keine vergleichbaren Builds dieser Art in der Vorperiode",
    noCompareIncomplete: "Build-Daten der Vorperiode unvollständig",
    noCompareLowSample: "unter {{min}} Builds in einer der beiden Perioden",
    fineText: "{{p50}}, {{rate}} fehlgeschlagen",
    fineWait: " · {{wait}} Warten, {{build}} Bauen",
    repoWait: "Typisch {{wait}} Warten auf einen Agent, {{build}} Bauen",
    group_one: "{{count}} Build aus {{repos}} Repos startete {{when}}",
    group_other: "{{count}} Builds aus {{repos}} Repos starteten {{when}}",
    groupSameDay: "am {{day}} zwischen {{from}} und {{to}}",
    groupSpan: "zwischen {{from}} und {{to}}",
    groupRest:
      ": das spricht für die Infrastruktur (Agents weg, wartender Input-Schritt), nicht für die Repos.",
    colPipeline: "Pipeline",
    colKind: "Art",
    colStart: "Start",
    colDuration: "Dauer",
    colWaiting: "Warten",
    colFactor: "× typisch",
    noAgent: "Der Build meldet nicht, wann er einen Agent bekam",
    eyebrow: "CI · Jenkins · Tekton · jede Build-Art für sich",
    title: "Wie schnell und verlässlich ist das Feedback?",
    fwEffectiveness: "Effectiveness, Treiber",
    dataFrom: "Build-Daten ab {{date}}; die Wochen davor sind leer.",
    unattributed_one:
      "{{count, num}} Build ohne Repo: Weder sein Commit noch sein Pipeline-Name nennt eines. In der Teamansicht gezählt, in keiner Produktansicht.",
    unattributed_other:
      "{{count, num}} Builds ohne Repo: Weder ihr Commit noch ihr Pipeline-Name nennt eines. In der Teamansicht gezählt, in keiner Produktansicht.",
    botRuns_one: "Bot-Builds: {{count, num}} Build, {{h}} CI-Zeit",
    botRuns_other: "Bot-Builds: {{count, num}} Builds, {{h}} CI-Zeit",
    botRunsFailed_one: ", davon {{count, num}} fehlgeschlagen ({{h}})",
    botRunsFailed_other: ", davon {{count, num}} fehlgeschlagen ({{h}})",
    botRunsRest:
      ". Kosten, kein Befund: in keiner anderen Zahl dieses Abschnitts.",
    noRuns: "Keine abgeschlossenen Builds im Zeitraum.",
    legend:
      "Erfolgreich: SUCCESS (Tekton: Succeeded, Completed). Fehlgeschlagen: FAILURE, UNSTABLE oder Failed. Ohne Ergebnis (ABORTED, NOT_BUILT): kein Build, der zählt, nur CI-Zeit; ein Jenkins-timeout() endet ebenfalls ABORTED.",
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
    noAgentRepo: "Keiner der Builds meldet, wann er einen Agent bekam",
    hotspotsRule:
      "Fehleranfälliger geworden: Quote mindestens {{failing}}× die der Vorperiode und {{points}} Prozentpunkte darüber, höchstens halbwegs zwischen ihr und 100 %, mit mindestens {{failures}} fehlgeschlagenen Builds. Langsamer geworden: typische Dauer mindestens {{slow}}× die der Vorperiode. Beides ab {{min}} Builds in beiden Perioden.",
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
        flaky: "Flaky (danach erfolgreich)",
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
        "schraffiert: fehlgeschlagen (flaky eingeschlossen), ohne Ausreisser",
      repoFailed: "{{repo}}: {{h}}, davon {{failed}} fehlgeschlagen",
      repoFailedShort: " · {{h}} fehlgeschlagen",
      reposOther_one: "{{count}} weiteres",
      reposOther_other: "{{count}} weitere",
      reposAria: "CI-Zeit nach Repo: {{parts}}",
      rule: "Fehlgeschlagen: FAILURE, UNSTABLE, Failed. Flaky: ebenso fehlgeschlagen, doch ein späterer Build derselben Pipeline war auf demselben Commit erfolgreich. Ohne Ergebnis: ABORTED, NOT_BUILT, Tekton None, Jenkins-Timeouts eingeschlossen. Ausreisser: {{outliers}} (ein Repo mit genug eigenen Builds zudem über seiner eigenen typischen Dauer). Ein Ausreisser zählt nur als Ausreisser, egal mit welchem Ergebnis; darum sind hier die Stunden der Ausreisser dabei, die die Karten oben weglassen. Die Bänder jeder Art zeigen ihre eigene Aufteilung, die Höhe eines Knotens nur die Menge: kein Vergleich zwischen den Arten.",
    },
  },
  quiver: {
    aria: "{{kind}}: jedes Repo von der Vorperiode zu jetzt, typische Dauer gegen Fehlerquote",
    xAxis: "Typische Dauer (log) →",
    yAxis: "↑ Fehlerquote",
    slower: "▲ ×{{factor}} Dauer",
    failingMore: "▲ Fehlerquote {{prev}} → {{rate}}",
    median: "Typisch",
    rate: "Fehlerquote",
    runs: "Builds",
    ci: "CI-Zeit",
    legend:
      "Pfeil: Vorperiode → jetzt; ein Punkt im oder am Kreis: kaum bewegt · voller Kreis ohne Punkt: nur die typische Dauer oder nur die Fehlerquote vergleichbar · Kreisfläche: Anteil an der CI-Zeit dieser Art · rot: langsamer oder fehleranfälliger geworden, wie unten · gestrichelt: kein Vergleich.",
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
    wastedWhy: "hoch heisst: viele Entwürfe werden reviewt und dann verworfen",
    perKloc: "Pro 1000 geänderte Zeilen",
    perKlocQuestion:
      "Was kostet das Review einer Änderung, die gemerged wurde?",
    trendOnly: "nur im Verlauf lesen",
    perKlocWhy: "steigt der Wert, werden Reviews pro Änderung teurer",
    byProduct: "Nach Produkt",
    byRepo: "Nach Repo",
    sub: "Alle Reviews im Zeitraum, neuestes Rollup pro PR",
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
    idleFor: "{{idle}} ohne Event",
    runningNote: " · Zeilen mit «Laufende Woche» zählen in keiner Zahl mit",
    firstOf: " · die ersten {{limit}} von {{count}}",
    noHits: "Keine Treffer für «{{q}}».",
    empty: "Keine Einträge im Zeitraum.",
    colPR: "PR",
    openPR: "PR in Bitbucket öffnen",
    colRepo: "Repo",
    colType: "Typ",
    colReview: "Review",
    colCycle: "Cycle",
    colToProd: "bis {{prod}}",
    colStatus: "Status",
    colAt: "Zeitpunkt",
    colApp: "Argo-App",
    colStage: "Stufe",
    colRevision: "Revision",
    colNewImages: "Neue Images",
    colCountsAs: "Zählt als",
    colPipeline: "Pipeline",
    colRun: "Build",
    colKind: "Art",
    colCommit: "Commit",
    colDuration: "Dauer",
    colImages: "Images",
    onProdAt: "Auf {{prod}} seit {{date}},",
    route: {
      compare: "gefunden über den Compare-Link im Release-Commit.",
      image: "gefunden über die Version, die ein Build gebaut hat.",
    },
    atMostTitle:
      "Höchstens: Vielleicht kam der PR schon mit einem früheren Deploy auf {{prod}}, das sich keinen PRs zuordnen lässt.",
    ...terms.de,
  },
  frameworks: {
    eyebrow: "Einordnung",
    title: "Welche Frameworks Riptide abdeckt",
    lead: "Ohne Umfrage messbar: zwei der fünf DORA-Metriken, DX Core 4 teilweise.",
    row: {
      freq: "Deploy-Frequenz",
      lead: "Lead Time bis PROD",
      stages: "Fluss nach Abschnitten",
      dsucc: "Deploy-Erfolg",
      review: "Review-Zeit, innert 24 h gemerged",
      touch: "Approval ohne Kommentar",
      merged: "PR Throughput",
      batch: "Batch Size",
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
      dsuccNot: "keine DORA-Metrik: zählt Sync-Fehler, nicht Change fail rate",
      diffsTeam:
        "Speed misst «Diffs pro Engineer»: Riptide zählt pro Team, nie pro Person",
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
