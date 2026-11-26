// The sticky header's jump links: the page's main questions, the one being
// read marked. German ids in both languages, so a shared link lands in the
// same place; the older section ids stay where they were.

import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

// The main questions, in page order.
export const SECTIONS = [
  "einschaetzung",
  "ausliefern",
  "fluss",
  "review",
  "build",
  "daten",
] as const;

// The rest of the page, behind "more".
export const MORE = ["deploys", "arbeitsmix", "kosten", "frameworks"] as const;

// The section being read: the last of ids whose top has passed the line
// just under the header. An IntersectionObserver says when to look again;
// none (tests, old browsers) leaves the first marked.
export function useActiveSection(
  ids: readonly string[],
  ready: boolean,
  headHeight: () => number,
): string {
  const [active, setActive] = useState(ids[0]);
  useEffect(() => {
    if (!ready || typeof IntersectionObserver === "undefined") return;
    const els = ids
      .map((id) => document.getElementById(id))
      .filter((e): e is HTMLElement => e != null);
    const pick = () => {
      const line = headHeight() + 24;
      let cur = ids[0];
      for (const el of els)
        if (el.getBoundingClientRect().top <= line) cur = el.id;
      // At the very bottom the last section is the one read, however short.
      if (
        window.innerHeight + window.scrollY >=
        document.documentElement.scrollHeight - 2
      )
        cur = els.at(-1)?.id ?? cur;
      setActive(cur);
    };
    const io = new IntersectionObserver(pick, {
      rootMargin: `-${headHeight()}px 0px -40% 0px`,
      threshold: [0, 0.25, 0.5, 0.75, 1],
    });
    els.forEach((el) => io.observe(el));
    window.addEventListener("scroll", pick, { passive: true });
    pick();
    return () => {
      io.disconnect();
      window.removeEventListener("scroll", pick);
    };
  }, [ids, ready, headHeight]);
  return active;
}

const LINK =
  "rounded-ui-sm px-2.5 py-1.5 text-[13.5px] whitespace-nowrap no-underline outline-none focus-visible:ring-2 focus-visible:ring-accent-strong";

export function SectionNav({ active }: { active: string }) {
  const { t } = useTranslation();
  const more = useRef<HTMLDetailsElement>(null);
  return (
    <nav
      aria-label={t("nav.label")}
      className="no-print flex flex-wrap items-center gap-0.5"
    >
      {SECTIONS.map((id) => (
        <a
          key={id}
          href={`#${id}`}
          aria-current={active === id ? "location" : undefined}
          className={`${LINK} relative ${
            active === id
              ? "bg-active text-ink after:absolute after:inset-x-2.5 after:-bottom-[9px] after:h-0.5 after:rounded-sm after:bg-accent"
              : "text-muted hover:bg-active hover:text-ink"
          }`}
        >
          {t(`nav.${id}`)}
        </a>
      ))}
      <details ref={more} className="relative">
        <summary
          className={`${LINK} cursor-pointer list-none text-muted hover:bg-active hover:text-ink [&::-webkit-details-marker]:hidden`}
        >
          {t("nav.more")} <span aria-hidden="true">▾</span>
        </summary>
        <div className="absolute top-[calc(100%+6px)] left-0 z-40 flex min-w-[170px] flex-col rounded-ui-sm border border-elevated-border bg-elevated p-1 shadow-menu">
          {MORE.map((id) => (
            <a
              key={id}
              href={`#${id}`}
              onClick={() => more.current?.removeAttribute("open")}
              className={`${LINK} text-ink-dim hover:bg-active hover:text-ink`}
            >
              {t(`nav.${id}`)}
            </a>
          ))}
        </div>
      </details>
    </nav>
  );
}
