# Ragas Evaluation

This folder evaluates the existing HR RAG recommendation flow with Ragas.

## 1. Start Services

Start the normal MySQL, Milvus, logic, and web services. The logic service must expose tool results for eval collection:

```bash
AI_SHOW_TOOL_RESULTS=true RAG_ENABLED=true ...
```

Keep this off in normal product usage.

## 2. Install Python Dependencies

```bash
python3 -m venv .venv-ragas
. .venv-ragas/bin/activate
pip install -r eval/ragas/requirements.txt
```

Ragas uses an evaluator LLM. Configure the provider expected by your installed Ragas version, commonly `OPENAI_API_KEY` and optionally model/base URL variables.

## 3. Collect Runs

Use an existing HR token:

```bash
python eval/ragas/collect_runs.py --token "$TOKEN"
```

Or login through the script:

```bash
RAGAS_EVAL_USERNAME=hr_user RAGAS_EVAL_PASSWORD=your_password \
python eval/ragas/collect_runs.py
```

Output:

```text
eval/ragas/outputs/rag_runs.jsonl
```

Each row contains `user_input`, model `response`, and `retrieved_contexts` extracted from `recommend_resumes_by_jd` or `semantic_search_resumes` tool results.

## 4. Run Ragas

```bash
python eval/ragas/run_ragas.py
```

Outputs:

```text
eval/ragas/outputs/ragas_scores.csv
eval/ragas/outputs/ragas_summary.csv
```

The default metrics are faithfulness, response relevancy, and context precision without reference. Add `reference` fields to `questions.jsonl` later if you want reference-based metrics.
