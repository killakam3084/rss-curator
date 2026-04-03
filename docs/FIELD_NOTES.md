# Field Notes — Exploratory Topics for Further Study

Topics that surfaced organically during rss-curator development.
Each entry includes the context in which it came up and pointers for deeper reading.

---

## Probability & Statistics

### Temperature in LLM Inference
**Context:** rss-curator scorer producing non-deterministic results (S12E02 scored 0.2, S12E03 scored 0.8 for identical technical profiles).

Temperature is a post-hoc scaling parameter applied to a model's output probability distribution *before* sampling the next token. It does not change the model's weights or knowledge — it controls how strictly the model adheres to its own confidence when making a choice.

- `temperature = 0` → always pick the argmax token; fully deterministic; same input = same output
- `temperature = 1` → sample proportionally from the raw distribution
- `temperature > 1` → flatten distribution; more creative, more inconsistent
- `temperature < 1` → sharpen distribution; more focused, less varied

**Key insight:** For classification / structured-output tasks (scoring, grading, routing), `temperature = 0` is almost always correct. For generative / creative tasks (summarisation, suggestions), some temperature adds useful diversity.

**Go deeper:**
- Softmax function and how temperature enters it: $P(x_i) = \frac{e^{z_i / T}}{\sum_j e^{z_j / T}}$
- Top-p (nucleus) sampling and top-k sampling as complementary controls
- "Calibration" in ML classifiers — does a model's stated confidence actually match its accuracy?

---

### Precision vs. Recall
**Context:** The NOVA/renovation false positive — `NOVA` as a substring matching `reNOVAtion`.

The classic tradeoff in information retrieval and classification:

- **Precision** — of everything the system flagged as a match, what fraction was actually correct?
- **Recall** — of everything that *was* a correct match, what fraction did the system find?

A loose substring matcher maximises recall (it doesn't miss much) at the cost of precision (it matches things it shouldn't). A strict exact-title matcher has high precision but poor recall (misses alternate titles, partial matches, spin-offs).

The goal is tuning the operating point for the cost asymmetry of your domain: in rss-curator, a false positive (downloading something unwanted) is more costly than a false miss (not seeing an episode until the next feed poll).

**Go deeper:**
- F1 score as the harmonic mean of precision and recall
- Precision-recall curves vs. ROC curves and when each is more informative
- Word-boundary matching (regex `\b`) as a simple precision improvement over raw substring

---

## Machine Learning / Inference

### Classifier Calibration
**Context:** Roadmapping the `match_confidence` signal — the scorer should be able to express uncertainty about whether a rule plausibly describes the content, separate from the release-quality score.

A model is *calibrated* if its confidence scores reflect real-world accuracy — i.e. when it says 0.9, it's right ~90% of the time. Most raw neural network classifiers are overconfident out of the box.

In rss-curator's context: if the scorer says `score: 0.85` for a torrent whose rule match is suspect, there's no way to distinguish "high quality release of the right show" from "high quality release of the wrong show that happened to match a broad rule." The `match_confidence` field is essentially adding a second calibrated output axis.

**Go deeper:**
- Platt scaling and isotonic regression as post-hoc calibration techniques
- Expected Calibration Error (ECE) as a metric
- Temperature scaling as a simple calibration method (yes, related to the sampling temperature concept above)

---

### Selection Bias in History Sampling
**Context:** The scorer's `sampleHistory` function — what 40 records from your accept/reject history get sent in the prompt directly shapes how the model reasons about new items.

If the sample is unbalanced (e.g. dominated by recent approvals of one show), the model may systematically over-score similar items not because they deserve it but because the context window is biased. Stratified sampling (current implementation) mitigates this, but doesn't eliminate it.

**Go deeper:**
- Stratified sampling vs. random sampling vs. importance sampling
- Recency bias and how to weight a time-decay factor into sample selection
- The broader problem: "in-context learning" in LLMs — few-shot examples in the prompt act as implicit training signal per-request

---

### Retrieval-Augmented Generation (RAG)
**Context:** Whether a small local model (e.g. `llama3.2:3b`) can generalise well enough for the Suggester, or whether it needs richer context injected at inference time.

RAG is the pattern of fetching relevant external knowledge at query time and injecting it into the prompt, rather than relying solely on what the model learned during training. For rss-curator:

- The Suggester could query a local TVDB/TMDb cache at suggestion time and inject franchise metadata, creator credits, and genre tags into the prompt — letting a small model reason about relationships it would otherwise miss
- This would let you keep a fast/cheap local model while getting quality closer to a much larger one

**Go deeper:**
- Vector embeddings and cosine similarity as the retrieval mechanism in most RAG systems
- FAISS, ChromaDB, pgvector as common vector stores
- The distinction between RAG and fine-tuning: RAG adds knowledge at inference time; fine-tuning bakes it into weights
- "Lost in the middle" problem — LLMs attend poorly to context in the middle of long prompts; retrieved chunks should go at the beginning or end

---

### Model Tiering / Routing
**Context:** `NewProviderFor(subsystem)` — using `llama3.2:1b` for enrichment, `llama3.2:3b` for scoring, reserving `llama3.1:8b` for the on-demand Suggester.

Not all inference tasks have the same cost/quality requirements. The pattern of routing different tasks to different-sized models based on latency budget and reasoning complexity is a form of "model routing" or "cascade inference."

**Go deeper:**
- FrugalGPT and similar papers on cost-optimal LLM cascade strategies
- Speculative decoding — using a small model to draft tokens that a large model verifies/corrects
- Mixture of Experts (MoE) as the architectural version of this idea baked into a single model

---

## Information Retrieval

### Word-Boundary Matching
**Context:** NOVA matching reNOVAtion; Yellowstone matching Marshals: A Yellowstone Story.

Substring matching is O(n) and simple but has no awareness of token boundaries. Word-boundary regex (`\b`) asserts position between a `\w` and a `\W` character (or start/end of string), which eliminates embedded-substring false positives without requiring exact-match semantics.

For show names with multiple words or special characters, the boundary check needs care:
- `\bNOVA\b` correctly rejects `reNOVAtion` and `NOVA2` while matching `NOVA`, `NOVA S53`, `(NOVA)`
- Parenthetical or subtitle patterns (e.g. `Marshals: A Yellowstone Story`) need the rule to match either the full canonical title or an anchored prefix, not a fragment anywhere in the string

**Go deeper:**
- Finite automata and how regex engines implement `\b`
- Fuzzy string matching (Levenshtein distance, Jaro-Winkler) for handling OCR-style title noise
- Canonical title normalisation (lowercasing, stripping articles "The"/"A", handling unicode) as a pre-matching step

---

## AI Prompt Engineering

### Compact Show-History Summaries
**Context:** AI scorer was suffering from recency bias — the last raw activity log entry (e.g. `[REJECT] Beachfront Bargain Hunt Renovation`) appeared immediately before the task instruction, causing the model to anchor on it as the subject to score rather than the actual candidate.

Rather than sending raw log lines, history is now aggregated into per-show `ShowSummary` structs and rendered as compact one-liners:

```
NOVA: +0 -1 | last:2026-03-12 | q=1080P | ex=Beachfront Bargain Hunt Renovation S12E03 1080p HEVC x265...
Tulsa King: +4 -0 | last:2026-03-07 | q=2160P | ex=Tulsa King S01E08 Adobe Walls 2160p AMZN WEB-DL DDP5 1 H...
```

This approach:
- Reduces token count significantly compared to raw log lines
- Eliminates the "last entry bias" — no single entry dominates proximity to the task instruction
- Provides richer signal per token (approval ratio, recency, quality preference, example title)
- Pins the candidate show's own history first, regardless of activity rank

**Go deeper:**
- "Lost in the middle" problem in LLMs — models attend best to content at the beginning and end of the context window; placing candidate last (immediately before the instruction) is intentional
- Few-shot prompting vs. history injection — the activity summaries act as implicit few-shot examples per-show
- Token budget management: compact representations allow more shows in context within the same `num_ctx` window

---

### Structured Output with Ollama
**Context:** llama3.2 was hallucinating a JSON Schema definition instead of a scored response when using `format: "json"` (loose JSON mode). The model inferred it should *describe* the schema rather than *populate* it.

Ollama supports passing a full JSON Schema object as the `format` field, which constrains the model to exactly the declared shape and types:

```json
{
  "type": "object",
  "properties": {
    "score": { "type": "number", "minimum": 0, "maximum": 1 },
    "reason": { "type": "string" },
    "match_confidence": { "type": "number", "minimum": 0, "maximum": 1 },
    "match_confidence_reason": { "type": "string" }
  },
  "required": ["score", "reason", "match_confidence", "match_confidence_reason"]
}
```

The `FormatSetter` interface was added to the `ai.Provider` layer so the scorer can configure structured output without changing the `Provider` interface signature — only providers that support it (currently `ollamaProvider`) implement it; the scorer uses a type assertion at construction time.

**Go deeper:**
- JSON Schema as a type system for LLM outputs — `minimum`/`maximum` on numeric fields acts as soft constraints; clamping in application code provides a hard guarantee
- Constrained decoding / grammar-based sampling as the mechanism behind structured outputs in local LLMs
- OpenAI's equivalent: `response_format: { type: "json_schema", json_schema: { ... } }`

---

## Observability

### Prometheus + Netdata for Host Metrics
**Context:** TrueNAS SCALE host running the curator stack; Prometheus scraping Netdata at `/api/v1/allmetrics?format=prometheus`; CPU, memory, and load average alerting.

Key Netdata metric names for Prometheus:

| Metric | Dimensions | Notes |
|---|---|---|
| `netdata_system_cpu_percentage_average` | `user`, `system`, `idle`, `iowait`, ... | Exclude `idle`; use `sum(...{dimension=~"user\|system"}) by (instance)` for active CPU |
| `netdata_system_ram_MiB_average` | `used`, `free`, `cached`, `buffers` | Use used/(used+free) for utilisation ratio |
| `netdata_system_load_load_average` | `load1`, `load5`, `load15` | `load1` is noisy; prefer `load5` or avg of all three for alerting |

**Key insights:**
- Direct `+` between metrics with different `dimension=` labels fails in PromQL — use `sum(...) by (instance)` to aggregate across dimensions first
- `load1` is highly variable; `load5` provides a better signal-to-noise ratio; average of all three gives the smoothest signal
- Load alert thresholds should be relative to CPU core count
- Place `alerts.yml` in your Prometheus config directory and reference it via `rule_files:` in `prometheus.yml`

**Go deeper:**
- Prometheus data model: labels as dimensions; why label cardinality matters for query performance
- `avg_over_time()` vs. `rate()` — use `avg_over_time` for gauges (CPU%, RAM); `rate()` for counters (bytes transferred)
- Alertmanager for routing, silencing, and grouping alerts from Prometheus rules

---

## Roadmap / Deferred Ideas

### qBittorrent Download Status Integration
**Context:** Queued-for-download torrents now get a `queued` status in the curator DB (written at queue time). The natural next question is: can curator surface actual download progress — downloading, stalled, seeding, completed — back in the UI?

**What would be needed:**
- A background poller in `internal/ops/` (e.g. `RunStatusSync`) that periodically calls `client.GetTorrents()` and matches results back to curator rows by magnet hash or torrent URL.
- New status values in the state machine: `downloading`, `stalled`, `seeding`, `completed` (or a narrower subset — just `completed` may be enough).
- `UpdateStatus` already exists and accepts any string; no schema migration needed beyond accepting new values in the UI and `DeleteOld` cleanup logic.
- The hard part is the **matching problem**: qBittorrent tracks by info-hash; curator rows store the original magnet/torrent URL. Extracting the hash from a magnet URI is straightforward (`xt=urn:btih:<hash>`); matching .torrent file URLs requires a HEAD/GET to retrieve the hash — adds latency and complexity.

**Design tension:**
- A poller creates a continuous dependency on qBittorrent availability; curator currently tolerates qBittorrent being down (queue calls fail gracefully).
- Polling interval vs. staleness: a 60s poll is probably fine for "completed" detection; overkill for "downloading" progress bars.
- Alternative: webhook/event-driven via qBittorrent's "run external program on completion" hook. Avoids polling but requires an inbound endpoint and careful auth.

**Recommended scope when tackled:**
1. Hash extraction from magnet URIs only (skip .torrent URL matching for now).
2. Single new status: `completed` — the most actionable signal.
3. `RunStatusSync` as a scheduled op (e.g. every 5 minutes), not a live SSE stream.
4. Surface `completed` as a badge/indicator on the queued tab — not a new 5th tab.

**Go deeper:**
- qBittorrent Web API: `GET /api/v2/torrents/info?filter=completed` for efficient polling
- Magnet URI format (RFC-unofficial): `magnet:?xt=urn:btih:<hex-or-base32-hash>&dn=...`
- Info-hash encoding: SHA-1 hex (40 chars) or Base32 (32 chars); go-qbittorrent normalises to lowercase hex

---

*Last updated: 2026-04-02*
