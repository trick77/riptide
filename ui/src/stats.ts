// percentile is Postgres' percentile_cont: linear interpolation between
// the two nearest ranks, so the UI's numbers match the README's queries.
// null on no values.
export function percentile(values: number[], p: number): number | null {
  if (values.length === 0) return null;
  const v = [...values].sort((a, b) => a - b);
  const rank = p * (v.length - 1);
  const lo = Math.floor(rank);
  const hi = Math.ceil(rank);
  return v[lo] + (v[hi] - v[lo]) * (rank - lo);
}

export const median = (values: number[]) => percentile(values, 0.5);

export function sum(values: number[]): number {
  return values.reduce((a, b) => a + b, 0);
}

// seconds between two ISO timestamps; null when either is missing.
export function between(from: string | null, to: string | null): number | null {
  if (!from || !to) return null;
  return (Date.parse(to) - Date.parse(from)) / 1000;
}
