import json
import logging
import os
import re
from typing import List, Optional

from fastapi import FastAPI
from pydantic import BaseModel, Field
from google import genai

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("promscale-agent")

app = FastAPI(title="PromScale LLM Decision Agent", version="0.0.2")


class AgentSignal(BaseModel):
    name: str
    value: str


class AgentRequest(BaseModel):
    scaler: str
    namespace: str
    currentReplicas: int = Field(ge=0)
    readyReplicas: int = Field(ge=0)
    minReplicas: int = Field(ge=0)
    maxReplicas: int = Field(ge=1)
    signals: List[AgentSignal] = []


class AgentResponse(BaseModel):
    desiredReplicas: int
    reason: str
    confidence: str


def _get_env(name: str, default: Optional[str] = None) -> str:
    v = os.getenv(name, default)
    if v is None or v == "":
        raise RuntimeError(f"Missing required env var: {name}")
    return v


def build_prompt(req: AgentRequest) -> str:
    signals_block = "\n".join([f"- {s.name}: {s.value}" for s in req.signals]) or "- (no signals)"

    return f"""
You are an autoscaling decision agent for a Kubernetes Operator (PromScale).

CONTEXT
- Scaler: {req.scaler}
- Namespace: {req.namespace}
- Current replicas: {req.currentReplicas}
- Ready replicas: {req.readyReplicas}
- Hard bounds: min={req.minReplicas}, max={req.maxReplicas}

OBSERVED SIGNALS
{signals_block}

DECISION RULES
1) Recommend an integer desired replica count.
2) desiredReplicas MUST be within [minReplicas, maxReplicas].
3) Avoid aggressive scale-down if readyReplicas < currentReplicas.
4) Prefer stability; avoid oscillations.
5) If signals are weak or ambiguous, prefer holding currentReplicas.

RETURN STRICT JSON ONLY (no markdown, no prose, no extra keys):
{{
  "desiredReplicas": <integer>,
  "reason": <string>,
  "confidence": <string between "0" and "1">
}}
""".strip()


def extract_json(text: str) -> dict:
    text = (text or "").strip()
    if not text:
        raise ValueError("empty model output")

    if text.startswith("{") and text.endswith("}"):
        return json.loads(text)

    m = re.search(r"\{.*\}", text, flags=re.DOTALL)
    if not m:
        raise ValueError("No JSON object found in model output")
    return json.loads(m.group(0))


def clamp(n: int, lo: int, hi: int) -> int:
    return max(lo, min(hi, n))


def confidence_str(x) -> str:
    try:
        f = float(str(x).strip())
    except Exception:
        return "0"
    if f != f:  # NaN
        return "0"
    f = max(0.0, min(1.0, f))
    s = str(round(f, 3)).rstrip("0").rstrip(".")
    return s if s else "0"


@app.get("/health")
def health():
    return {"ok": True}


@app.post("/decide", response_model=AgentResponse)
def decide(req: AgentRequest):
    api_key = _get_env("GOOGLE_API_KEY")
    model = os.getenv("GEMINI_MODEL", "gemini-2.0-flash")
    prompt = build_prompt(req)
    fallback_desired = clamp(req.currentReplicas, req.minReplicas, req.maxReplicas)

    try:
        client = genai.Client(api_key=api_key)

        resp = client.models.generate_content(
            model=model,
            contents=prompt,
        )

        text = (resp.text or "").strip()
        logger.info("Gemini returned text len=%d", len(text))

        data = extract_json(text)

        desired = int(data.get("desiredReplicas"))
        reason = str(data.get("reason", "")).strip()
        conf = confidence_str(data.get("confidence", "0"))

        desired = clamp(desired, req.minReplicas, req.maxReplicas)
        if not reason:
            reason = "LLM recommended replicas based on provided signals"
        if len(reason) > 255:
            reason = reason[:255]

        return AgentResponse(desiredReplicas=desired, reason=reason, confidence=conf)

    except Exception as e:
        logger.exception("LLM call failed; returning fallback. error=%s", type(e).__name__)

        return AgentResponse(
            desiredReplicas=fallback_desired,
            reason=f"LLM decision failed; holding currentReplicas; error={type(e).__name__}",
            confidence="0",
        )