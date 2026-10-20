"""OpenAI-compatible hosted chat-completions tool loop."""
import argparse, json, os, urllib.request
import sys
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from cellbox import Client
from sdk.tools.cellbox_tools import dispatch_json

def post(url, token, payload):
    req=urllib.request.Request(url,data=json.dumps(payload).encode(),headers={"Authorization":f"Bearer {token}","Content-Type":"application/json"})
    with urllib.request.urlopen(req,timeout=120) as r:return json.load(r)

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument("--fake-model",action="store_true");args=parser.parse_args()
    if args.fake_model:
      with Client(os.getenv("CELLBOX_URL","http://127.0.0.1:8080"),os.environ["CELLBOX_KEY"]) as client:
        with client.sessions.create("python:3.12-slim",memory_mb=512,network_policy={"mode":"none"}) as box:
          print(dispatch_json(box,"run_command",{"command":["python","-c","print(6 * 7)"]}))
      return
    token=os.environ["OPENAI_API_KEY"];endpoint=os.getenv("OPENAI_BASE_URL","https://api.openai.com/v1").rstrip("/")+"/chat/completions"
    tools=json.loads((Path(__file__).parents[1]/"sdk/tools/openai.json").read_text())[:4]
    with Client(os.getenv("CELLBOX_URL","http://127.0.0.1:8080"),os.environ["CELLBOX_KEY"]) as client:
      with client.sessions.create("python:3.12-slim",memory_mb=512,network_policy={"mode":"none"}) as box:
        messages=[{"role":"user","content":"Create /tmp/answer.py containing print(6 * 7), run it, and report its output."}]
        for _ in range(8):
          choice=post(endpoint,token,{"model":os.getenv("OPENAI_MODEL","gpt-4o-mini"),"messages":messages,"tools":tools,"tool_choice":"auto"})["choices"][0]["message"]
          messages.append(choice)
          calls=choice.get("tool_calls",[])
          if not calls: print(choice.get("content", ""));return
          for call in calls:
            args=json.loads(call["function"].get("arguments") or "{}")
            result=dispatch_json(box,call["function"]["name"],args)
            messages.append({"role":"tool","tool_call_id":call["id"],"content":result})
        raise SystemExit("model tool-call limit reached")
if __name__=="__main__":main()
