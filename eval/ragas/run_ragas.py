#!/usr/bin/env python3
"""Run Ragas metrics over collected RAG responses."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
from typing import Any

import pandas as pd
from datasets import Dataset
from ragas import evaluate

try:
    from dotenv import load_dotenv
except ImportError:  # pragma: no cover - optional runtime dependency
    load_dotenv = None


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    with path.open("r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    return rows


def build_metrics() -> list[Any]:
    try:
        from ragas.metrics import (
            Faithfulness,
            LLMContextPrecisionWithoutReference,
            ResponseRelevancy,
        )

        return [
            Faithfulness(),
            ResponseRelevancy(),
            LLMContextPrecisionWithoutReference(),
        ]
    except ImportError:
        from ragas.metrics import answer_relevancy, context_precision, faithfulness

        return [faithfulness, answer_relevancy, context_precision]


def env_first(*names: str) -> str:
    for name in names:
        value = os.getenv(name, "").strip()
        if value:
            return value
    return ""


def build_evaluator_llm() -> Any | None:
    api_key = env_first("RAGAS_EVAL_LLM_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY")
    if not api_key:
        return None
    model = env_first("RAGAS_EVAL_LLM_MODEL", "OPENAI_MODEL", "DEEPSEEK_MODEL") or "deepseek-chat"
    base_url = env_first("RAGAS_EVAL_LLM_BASE_URL", "OPENAI_BASE_URL", "DEEPSEEK_BASE_URL")
    from langchain_openai import ChatOpenAI
    from ragas.llms import LangchainLLMWrapper

    kwargs: dict[str, Any] = {"api_key": api_key, "model": model, "temperature": 0}
    if base_url:
        kwargs["base_url"] = base_url
    return LangchainLLMWrapper(ChatOpenAI(**kwargs))


def build_evaluator_embeddings() -> Any | None:
    api_key = env_first("RAGAS_EVAL_EMBEDDING_API_KEY", "OPENAI_EMBEDDING_API_KEY", "DASHSCOPE_API_KEY")
    if not api_key:
        return None
    model = env_first("RAGAS_EVAL_EMBEDDING_MODEL", "OPENAI_EMBEDDING_MODEL", "RAG_EMBEDDING_MODEL") or "text-embedding-v3"
    base_url = env_first("RAGAS_EVAL_EMBEDDING_BASE_URL", "OPENAI_EMBEDDING_BASE_URL", "RAG_EMBEDDING_ENDPOINT")
    from langchain_openai import OpenAIEmbeddings
    from ragas.embeddings import LangchainEmbeddingsWrapper

    kwargs: dict[str, Any] = {"api_key": api_key, "model": model, "check_embedding_ctx_length": False}
    if base_url:
        kwargs["base_url"] = base_url
    return LangchainEmbeddingsWrapper(OpenAIEmbeddings(**kwargs))


def main() -> int:
    if load_dotenv is not None:
        load_dotenv()

    parser = argparse.ArgumentParser()
    parser.add_argument("--input", default="eval/ragas/outputs/rag_runs.jsonl")
    parser.add_argument("--output", default="eval/ragas/outputs/ragas_scores.csv")
    parser.add_argument("--summary", default="eval/ragas/outputs/ragas_summary.csv")
    args = parser.parse_args()

    rows = read_jsonl(Path(args.input))
    if not rows:
        raise SystemExit("input file has no rows")

    eval_rows = []
    for row in rows:
        contexts = row.get("retrieved_contexts") or []
        if not contexts:
            raise SystemExit(
                "at least one row has empty retrieved_contexts; start logic service with "
                "AI_SHOW_TOOL_RESULTS=true and make sure the agent calls a RAG tool"
            )
        eval_row = {
            "user_input": row["user_input"],
            "response": row["response"],
            "retrieved_contexts": contexts,
            "question": row["user_input"],
            "answer": row["response"],
            "contexts": contexts,
        }
        if row.get("reference"):
            eval_row["reference"] = row["reference"]
        eval_rows.append(eval_row)

    result = evaluate(
        Dataset.from_list(eval_rows),
        metrics=build_metrics(),
        llm=build_evaluator_llm(),
        embeddings=build_evaluator_embeddings(),
    )
    df = result.to_pandas()

    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    df.to_csv(output, index=False)

    summary = df.mean(numeric_only=True).to_frame("mean").reset_index()
    summary.to_csv(args.summary, index=False)
    print(summary.to_string(index=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
