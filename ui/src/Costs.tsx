import { useTranslation } from "react-i18next";
import type { Metrics } from "./api";
import { BarList } from "./charts";
import { money, pct } from "./format";
import { Card, CardHead, Direction, SectionHead, Tile } from "./ui";

export function Costs({ m }: { m: Metrics }) {
  const { t } = useTranslation();
  const c = m.costs;
  const cmp = m.comparison;
  return (
    <section id="kosten" aria-labelledby="h-kosten" className="mt-10">
      <SectionHead
        id="h-kosten"
        eyebrow={t("costs.eyebrow")}
        title={t("costs.title")}
        lead={t("costs.lead")}
      />
      <div className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
        <Tile
          label={t("costs.spend")}
          value={money(c.total)}
          note={
            t("costs.spendNote", {
              merged: money(c.merged),
              count: c.merged_reviews,
            }) +
            (c.promotion_reviews > 0
              ? ` · ${t("costs.promotionNote", {
                  promotion: money(c.promotion),
                  count: c.promotion_reviews,
                })}`
              : "")
          }
        />
        <Tile
          label={t("costs.wasted")}
          question={t("costs.wastedQuestion")}
          value={pct(c.wasted_share)}
          unit={money(c.wasted)}
          direction={
            <Direction
              label={t("common.noDirection")}
              why={t("costs.wastedWhy")}
            />
          }
          compare={{
            change: cmp.wasted_share,
            format: pct,
          }}
        />
        <Tile
          label={t("costs.perKloc")}
          question={t("costs.perKlocQuestion")}
          value={c.per_kloc == null ? "–" : money(c.per_kloc)}
          direction={
            <Direction
              label={t("costs.trendOnly")}
              why={t("costs.perKlocWhy")}
            />
          }
          compare={{
            change: cmp.cost_per_kloc,
            format: money,
          }}
        />
      </div>
      <Card className="mt-3">
        <CardHead
          title={m.product == null ? t("costs.byProduct") : t("costs.byRepo")}
          sub={t("costs.sub")}
        />
        {c.rows.length ? (
          <BarList rows={c.rows} format={money} />
        ) : (
          <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
            {t("costs.none")}
          </p>
        )}
        <div className="mt-3.5 text-xs text-muted">
          {t("costs.reviewedRepos", { n: c.reviewed_repos })}
        </div>
      </Card>
    </section>
  );
}
