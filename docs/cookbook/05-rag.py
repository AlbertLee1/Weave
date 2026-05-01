"""Cookbook chapter 5 — RAG (retrieval-augmented generation).

Demonstrates grounding an LLM answer in Weave-stored documents:

1. Retrieve top-K relevant rows via ``client.objects.search``.
2. Format them into numbered snippets so the model can cite by index.
3. Hand the snippets + question to an LLM and stream the answer.

The ``call_llm`` function ships as a deterministic stub so the recipe
is runnable without network access. Replace it with your provider of
choice (Anthropic, OpenAI, Ollama, etc.) without touching the
retrieval / formatting halves.

Run::

    export WEAVE_BASE_URL=http://localhost:9117
    export WEAVE_TOKEN=...
    export WEAVE_ONTOLOGY=northwind
    export WEAVE_DOC_TYPE=Document
    python3 docs/cookbook/05-rag.py "What is the return policy?"
"""
from __future__ import annotations

import os
import sys
from typing import List

from weave_client import Client, WeaveError


SYSTEM = (
    "Answer the user's question using ONLY the snippets below. "
    "After every claim, cite the snippet number in square brackets, e.g. [2]. "
    "If the snippets don't contain the answer, say 'I don't know' — do not invent."
)


def retrieve(
    client: Client,
    ontology: str,
    object_type: str,
    query: str,
    k: int = 5,
) -> List[dict]:
    """Run a where-clause search to fetch the top-K relevant documents.

    Adapt the field names (``body``, ``title``) to whatever your
    Document ObjectType actually stores.
    """
    page = client.objects.search(
        ontology,
        object_type,
        where={
            "type": "containsAllTerms",
            "field": "body",
            "value": query,
        },
        select=["__primaryKey", "title", "body"],
        page_size=k,
    )
    return list(page.data)


def format_snippets(rows: List[dict]) -> str:
    """Number snippets so the LLM can cite by ``[i]`` index."""
    parts: List[str] = []
    for i, row in enumerate(rows, start=1):
        title = row.get("title") or "(untitled)"
        body = (row.get("body") or "").strip()
        if len(body) > 800:
            body = body[:800].rsplit(". ", 1)[0] + "..."
        parts.append(f"[{i}] {title}\n{body}\n(rid: {row.get('__primaryKey', '')})")
    return "\n\n".join(parts)


def build_messages(snippets: str, question: str) -> List[dict]:
    return [
        {"role": "system", "content": SYSTEM},
        {
            "role": "user",
            "content": f"<context>\n{snippets}\n</context>\n\nQuestion: {question}",
        },
    ]


def call_llm(messages: List[dict]) -> str:
    """Stub LLM caller. Replace with the SDK of your choice.

    Example with Anthropic::

        import anthropic
        def call_llm(messages):
            client = anthropic.Anthropic()
            resp = client.messages.create(
                model="claude-opus-4-7",
                max_tokens=1024,
                system=messages[0]["content"],
                messages=messages[1:],
            )
            return resp.content[0].text
    """
    user = next((m for m in messages if m["role"] == "user"), None)
    snippet_count = (user or {}).get("content", "").count("\n[")
    if snippet_count == 0:
        return "I don't know."
    return (
        f"(stub answer — replace call_llm with a real provider) "
        f"Cited snippets: " + ", ".join(f"[{i}]" for i in range(1, snippet_count + 1))
    )


def rag_answer(
    client: Client,
    ontology: str,
    object_type: str,
    question: str,
) -> str:
    rows = retrieve(client, ontology, object_type, question, k=5)
    if not rows:
        return "No relevant documents found."
    snippets = format_snippets(rows)
    messages = build_messages(snippets, question)
    return call_llm(messages)


def main(argv: List[str]) -> int:
    base_url = os.environ.get("WEAVE_BASE_URL", "http://localhost:9117")
    token = os.environ.get("WEAVE_TOKEN") or None
    ontology = os.environ.get("WEAVE_ONTOLOGY", "northwind")
    doc_type = os.environ.get("WEAVE_DOC_TYPE", "Document")

    question = " ".join(argv[1:]) or "What is the return policy?"

    with Client(base_url, access_token=token) as client:
        try:
            answer = rag_answer(client, ontology, doc_type, question)
        except WeaveError as err:
            print(f"weave error: {err}", file=sys.stderr)
            return 1

    print(f"Q: {question}")
    print(f"A: {answer}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
