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

## Systems / Architecture

### Async Job Queues
**Context:** Deferred roadmap item — AI scoring currently blocks the feed-processing loop; a queue would decouple ingestion from inference.

The producer-consumer pattern: a fast producer (feed parser) enqueues work items; one or more slow consumers (AI scorers) drain the queue at their own pace. Decoupling means the feed loop never blocks on inference latency.

**Go deeper:**
- Go channels as in-process queues; when to use buffered vs. unbuffered
- `errgroup` and worker pool patterns in Go for bounded concurrency
- At-least-once vs. exactly-once delivery semantics and why they matter for idempotent operations like scoring

---

*Last updated: 2026-03-11*
