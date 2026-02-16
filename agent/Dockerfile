FROM python:3.11-slim

WORKDIR /app

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

COPY requirements.txt .
COPY agent.py .
RUN pip install --no-cache-dir -r requirements.txt

EXPOSE 8081
CMD ["uvicorn", "agent:app", "--host", "0.0.0.0", "--port", "8081"]