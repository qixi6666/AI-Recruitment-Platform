#!/usr/bin/env python3
"""Collect RAG responses and retrieved contexts for Ragas evaluation.

The logic service must be started with AI_SHOW_TOOL_RESULTS=true so the
HTTP response context includes tool call results.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any

import requests


DEFAULT_BASE_URL = "http://localhost:8080/api/v1"


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    with path.open("r", encoding="utf-8") as f:
        for line_no, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            try:
                rows.append(json.loads(line))
            except json.JSONDecodeError as exc:
                raise SystemExit(f"{path}:{line_no}: invalid JSON: {exc}") from exc
    return rows


def write_jsonl(path: Path, rows: list[dict[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        for row in rows:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")


def normalize_base_url(value: str) -> str:
    value = value.rstrip("/")
    if not value.endswith("/api/v1"):
        value = value + "/api/v1"
    return value


def login(base_url: str, username: str, password: str) -> str:
    resp = requests.post(
        f"{base_url}/auth/login",
        json={"username": username, "password": password, "role": "hr"},
        timeout=30,
    )
    resp.raise_for_status()
    payload = resp.json().get("data", resp.json())
    token = payload.get("token")
    if not token:
        raise RuntimeError("login response did not include a token")
    return token


def parse_json_maybe(value: Any) -> Any:
    if not isinstance(value, str):
        return value
    try:
        return json.loads(value)
    except json.JSONDecodeError:
        return value


def evidence_text(candidate: dict[str, Any], item: dict[str, Any]) -> str:
    pieces = []
    name = candidate.get("candidate_name") or item.get("candidate_name")
    cid = candidate.get("candidate_id") or item.get("candidate_id")
    if name or cid:
        pieces.append(f"candidate={name or ''} id={cid or ''}".strip())
    section = item.get("section_title") or item.get("section_type")
    if section:
        pieces.append(f"section={section}")
    content = item.get("content") or item.get("evidence_text") or ""
    pieces.append(str(content))
    return " | ".join(piece for piece in pieces if piece).strip()


def contexts_from_tool_result(result: Any) -> list[str]:
    result = parse_json_maybe(result)
    contexts: list[str] = []
    if not isinstance(result, dict):
        return contexts

    for candidate in result.get("candidates") or []:
        if not isinstance(candidate, dict):
            continue
        for item in candidate.get("evidence") or []:
            if isinstance(item, dict):
                text = evidence_text(candidate, item)
                if text:
                    contexts.append(text)

    for item in result.get("items") or []:
        if isinstance(item, dict):
            text = evidence_text({}, item)
            if text:
                contexts.append(text)

    return contexts


def extract_contexts(context: dict[str, Any]) -> tuple[list[str], list[dict[str, Any]]]:
    tool_calls_raw = context.get("tool_calls", "[]")
    tool_calls = parse_json_maybe(tool_calls_raw)
    if not isinstance(tool_calls, list):
        tool_calls = []

    contexts: list[str] = []
    for call in tool_calls:
        if not isinstance(call, dict):
            continue
        if call.get("name") not in {"recommend_resumes_by_jd", "semantic_search_resumes"}:
            continue
        contexts.extend(contexts_from_tool_result(call.get("result")))

    seen: set[str] = set()
    unique_contexts: list[str] = []
    for text in contexts:
        text = " ".join(str(text).split())
        if text and text not in seen:
            seen.add(text)
            unique_contexts.append(text)
    return unique_contexts, tool_calls


def ask(base_url: str, token: str, question: str) -> dict[str, Any]:
    resp = requests.post(
        f"{base_url}/hr/ai/chat",
        headers={"Authorization": f"Bearer {token}"},
        json={"question": question},
        timeout=120,
    )
    resp.raise_for_status()
    payload = resp.json().get("data", resp.json())
    answer = payload.get("answer", "")
    context = payload.get("context") or {}
    contexts, tool_calls = extract_contexts(context)
    return {
        "response": answer,
        "retrieved_contexts": contexts,
        "used_tools": context.get("used_tools", ""),
        "tool_calls": tool_calls,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", default="eval/ragas/questions.jsonl")
    parser.add_argument("--output", default="eval/ragas/outputs/rag_runs.jsonl")
    parser.add_argument("--base-url", default=os.getenv("RAGAS_EVAL_BASE_URL", DEFAULT_BASE_URL))
    parser.add_argument("--token", default=os.getenv("RAGAS_EVAL_TOKEN", ""))
    parser.add_argument("--username", default=os.getenv("RAGAS_EVAL_USERNAME", ""))
    parser.add_argument("--password", default=os.getenv("RAGAS_EVAL_PASSWORD", ""))
    args = parser.parse_args()

    base_url = normalize_base_url(args.base_url)
    token = args.token
    if not token:
        if not args.username or not args.password:
            raise SystemExit("Provide --token or set RAGAS_EVAL_USERNAME/RAGAS_EVAL_PASSWORD.")
        token = login(base_url, args.username, args.password)

    outputs: list[dict[str, Any]] = []
    for row in read_jsonl(Path(args.input)):
        question = row.get("user_input") or row.get("question")
        if not question:
            raise SystemExit(f"missing user_input/question in row: {row}")
        result = ask(base_url, token, question)
        out = {
            "query_id": row.get("query_id", ""),
            "user_input": question,
            "response": result["response"],
            "retrieved_contexts": result["retrieved_contexts"],
            "used_tools": result["used_tools"],
        }
        if "reference" in row:
            out["reference"] = row["reference"]
        outputs.append(out)
        print(
            f"{out['query_id'] or question}: contexts={len(out['retrieved_contexts'])} "
            f"tools={out['used_tools']}",
            file=sys.stderr,
        )

    write_jsonl(Path(args.output), outputs)
    print(f"wrote {len(outputs)} rows to {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
