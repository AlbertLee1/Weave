# Chapter 5 — RAG (Retrieval-Augmented Generation)

Use Weave as the **R** in RAG: retrieve grounded snippets from your
ontology, hand them to an LLM, and cite the originals back in the
answer. The recipe is intentionally LLM-agnostic — swap the
`call_llm()` function for your provider of choice (Anthropic, OpenAI,
local Ollama, an internal endpoint).

## Architecture

```
user query → search Weave  → top-K WireObjects → format snippets
                                                       ↓
                                                  LLM call
                                                       ↓
                                              answer + citations
```

Weave's `where`-clause search is the cheapest retrieval primitive: BM25
text scoring on Bleve indexes, no vector store required. For semantic
search, layer an embedding store on top and store the doc RID + chunk
offset as a property; the same recipe applies.

## Choosing the search shape

| Goal | Operator | Notes |
|---|---|---|
| Phrase match in a Document body | `containsAllTerms` | Tokenises the user query |
| Field equality (e.g. `country = "Germany"`) | `eq` | For structured filters |
| Approximate matching ("Kafca" → Kafka) | `fuzzy` | Single-token, Levenshtein ≤ 2 |
| Phrase with slop ("quick fox" within 3 tokens) | `phrase` (US-233) | `~N` slop syntax |
| Composition | `and` / `or` / `not` | Boolean tree |

Combine them with `and`/`or` to filter by tenant, security marking, or
date range before scoring relevance.

## Retrieval call

```python
def retrieve(client, ontology: str, object_type: str, query: str, k: int = 5):
    page = client.objects.search(
        ontology, object_type,
        where={
            "type": "and",
            "value": [
                {"type": "containsAllTerms", "field": "body", "value": query},
                {"type": "eq", "field": "marking", "value": "PUBLIC"},
            ],
        },
        select=["__primaryKey", "title", "body"],
        page_size=k,
    )
    return page.data
```

Always project the primary key and the human-readable identifier
(`title`, `name`, `id`) into the snippets so the model can cite
something stable. Don't dump every property — token budget is precious.

## Snippet formatting

Each retrieved row becomes one numbered context block. Keep the format
mechanical — the LLM is going to copy these block numbers back as
citations:

```python
def format_snippets(rows: list[dict]) -> str:
    parts = []
    for i, row in enumerate(rows, start=1):
        title = row.get("title", "(untitled)")
        body = (row.get("body") or "").strip()
        # Trim each snippet to ~800 chars so the prompt budget holds
        body = body[:800].rsplit(". ", 1)[0] + "..."
        parts.append(f"[{i}] {title}\n{body}\n(rid: {row['__primaryKey']})")
    return "\n\n".join(parts)
```

## Prompting

```python
SYSTEM = (
    "Answer the user's question using ONLY the snippets below. "
    "After every claim, cite the snippet number in square brackets, e.g. [2]. "
    "If the snippets don't contain the answer, say 'I don't know' — do not invent."
)

def build_messages(snippets: str, question: str) -> list[dict]:
    return [
        {"role": "system", "content": SYSTEM},
        {"role": "user", "content": f"<context>\n{snippets}\n</context>\n\nQuestion: {question}"},
    ]
```

Two non-obvious rules:

1. **No vague "use this context" — bake the numbering scheme into the
   prompt.** If you ask the model to "ground in the docs" without giving
   it a citation grammar, it'll cite whatever it likes and you can't
   verify.
2. **Refusal beats hallucination.** The "say I don't know" clause is
   what stops the model from synthesising plausible-but-wrong answers
   from its training data when the retrieval miss.

## Putting it together

```python
def rag_answer(client, llm, ontology: str, question: str) -> str:
    rows = retrieve(client, ontology, "Document", question, k=5)
    if not rows:
        return "No relevant documents found."
    snippets = format_snippets(rows)
    messages = build_messages(snippets, question)
    return llm(messages)
```

`llm(messages)` is the seam where you plug in the provider. The script
ships a stubbed implementation so the recipe is runnable without
network access; replace it with `anthropic.Anthropic().messages.create`
or equivalent.

## Streaming results

If your LLM provider streams tokens, route them straight through —
don't buffer the whole answer before emitting. Pair this with
[Chapter 4](04-subscription.md) when you want answers to update live as
new documents arrive: subscribe to the source ObjectSet, re-run the
query each time the snippet set changes, and stream the new answer.

## Common pitfalls

- **Re-embedding on every query.** If you layer vectors on top, embed
  once at write time (Action rule produces an `embedding[]` property)
  and reuse on read.
- **Ignoring marking-based access control.** RAG bypasses the UI's
  per-row policy filter unless you pass the user's token. Always call
  `Client(..., access_token=user_token)` so the search obeys the same
  marking rules the rest of the app does.
- **Stuffing too much context.** Five 400-token snippets beat fifty
  20-token ones — quality compounds, breadth doesn't.
- **Dropping citations.** If you don't render them in the UI, the user
  can't fact-check; the answer is back to opaque.

See [`05-rag.py`](05-rag.py) for an end-to-end recipe with a stubbed
LLM you can swap for a real provider.
