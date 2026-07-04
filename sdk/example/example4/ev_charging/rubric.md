
You are reviewing a research brief at /mnt/session/outputs/brief.md against a coverage checklist and verifying its citations. The writer was told the seven topics to cover; this rubric defines what counts as sufficient coverage for each topic, and how to verify citations.

COVERAGE CHECKLIST. Each item has a specific area:
  1. Capex range: a dollar range for installed cost per DC fast-charging stall or station.
  2. Demand charges: quantified impact on opex (a $/kW figure or a % of operating cost).
  3. Utilization breakeven: a breakeven or target utilization threshold (% or kWh/day).
  4. Subsidy programs: NEVI or another public funding program, named.
  5. Named operator: the GAAP net income or net loss from a specific public charging operator's most recent 10-K or 10-Q, and the citation for it must be the SEC filing itself (sec.gov), not a press release, earnings-call recap, or news article.
  6. Contrarian source: at least one cited source whose thesis is that the economics are unfavorable or structurally challenged.
  7. Cost split: a hardware vs soft-cost (install, permitting, grid) breakdown or ratio.

CITATION CHECK. For every [n] entry in the Sources section:
  a. LIVE: Fetch the URL with web_fetch. Mark LIVE only if web_fetch returns the readable page directly. Mark DEAD if 404, parked, login-walled, paywalled, returns a bot-block/403, or renders only via JavaScript. Do NOT corroborate via mirrors, reposts, or search snippets; the cited URL itself must fetch.
  b. VERBATIM: Search the fetched page for the quoted string. Mark QUOTE_MATCH if the exact string appears (treat curly vs straight quotes as equivalent); NOT_FOUND otherwise.
  c. SUPPORTS CLAIM: Mark SUPPORTS_CLAIM if the quoted passage actually backs the claim it's cited on in the brief; UNSUPPORTED if it's tangential, contradicts the claim, or is just a general statement of fact.

OUTPUT FORMAT:

Line 1: Coverage N/7. Citations M/K verified.

Then, for each failed item in the coverage checklist, create a new bullet, name the item and say what specific bar it failed in one sentence max per bullet. For example: "Item 3 Utilization breakeven - MISSING. <what's missing>".

Then, for each failed citation, create a new bullet with the format: "[n] domain - REASON. <what's wrong and what to do>". One sentence max per bullet. For example: "[3] evgo.com - DEAD. The URL returns a 403 error and appears to be behind a bot block. No mirrors or reposts; the cited URL itself must fetch."
