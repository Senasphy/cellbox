"""Small deterministic eval runner. Each task is isolated in its own session."""
import json, os
from concurrent.futures import ThreadPoolExecutor
from cellbox import Client

def main():
    tasks=["print(1 + 1)","print('cellbox')","print(sum(range(5)))"]
    def run(task):
        with Client(os.getenv("CELLBOX_URL","http://127.0.0.1:8080"),os.environ["CELLBOX_KEY"]) as c:
            with c.sessions.create("python:3.12-slim",memory_mb=512,network_policy={"mode":"none"}) as s:
                return {"task":task,"result":s.exec(["python","-c",task]).stdout.strip()}
    with ThreadPoolExecutor(max_workers=3) as pool: results=list(pool.map(run,tasks))
    print(json.dumps({"results":results,"passed":len(results)},indent=2))
if __name__=="__main__":main()
