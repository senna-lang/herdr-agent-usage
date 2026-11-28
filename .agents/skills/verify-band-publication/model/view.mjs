export const title = "Sidebar publication: one complete row after publishers finish";
export const state = ({ tokens, pc }) => {
  const finished = Object.values(pc).every(value => value === 4);
  const rows = [tokens.limit_high && "comfortable row", tokens.limit_low && "critical row"].filter(Boolean);
  const visible = rows.length === 2 ? "contradictory rows together" : rows[0] || "empty sidebar";
  return `${finished ? "Publishers finished" : "Publishing"}: ${visible}`;
};
const labels = ["read the current sidebar", "update the comfortable band", "update the middle band", "update the critical band", "publication finished"];
export const step = (_action, from, to) => {
  const writer = Object.keys(to.pc).find(name => to.pc[name] !== from.pc[name]);
  const stage = writer === undefined ? 4 : from.pc[writer];
  if (!labels[stage]) throw new Error(`Unmapped publication stage: ${stage}`);
  return `${writer || "All publishers"}: ${labels[stage]}`;
};
