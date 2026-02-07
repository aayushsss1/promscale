from fastapi import FastAPI
from prometheus_client import Gauge, Counter, generate_latest, CONTENT_TYPE_LATEST
from starlette.responses import Response

app = FastAPI()

queue_depth = Gauge("mock_queue_depth", "Mock queue depth representing pending requests")
http_requests = Counter("mock_http_requests_total", "Total HTTP requests", ["path"])

QUEUE = 0

@app.get("/health")
async def health():
    http_requests.labels(path="/health").inc()
    return {"ok": True}

@app.post("/increase")
async def increase(n: int = 1):
    global QUEUE
    http_requests.labels(path="/increase").inc()
    QUEUE = max(0, QUEUE + n)
    queue_depth.set(QUEUE)
    return {"queue_depth": QUEUE}

@app.post("/remove")
async def remove(n: int = 1):
    global QUEUE
    http_requests.labels(path="/remove").inc()
    QUEUE = max(0, QUEUE - n)
    queue_depth.set(QUEUE)
    return {"queue_depth": QUEUE}

@app.get("/metrics")
async def metrics():
    return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)