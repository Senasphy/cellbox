"""Local Ollama tool loop; no hosted API key is needed."""
import argparse, json, os, sys, urllib.request
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from cellbox import Client
from sdk.tools.cellbox_tools import dispatch_json

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument("--fake-model",action="store_true");args=parser.parse_args()
    if args.fake_model:
      with Client(os.getenv("CELLBOX_URL","http://127.0.0.1:8080"),os.environ["CELLBOX_KEY"]) as client:
        with client.sessions.create("python:3.12-slim",memory_mb=512,network_policy={"mode":"none"}) as box:
          print(dispatch_json(box,"run_command",{"command":["python","-c","print(6 * 7)"]}))
      return
    url=os.getenv("OLLAMA_URL","http://127.0.0.1:11434").rstrip("/")+"/api/chat"
    tools=json.loads((Path(__file__).parents[1]/"sdk/tools/openai.json").read_text())[:4]
    model=os.getenv("OLLAMA_MODEL","qwen2.5:7b")
    with Client(os.getenv("CELLBOX_URL","http://127.0.0.1:8080"),os.environ["CELLBOX_KEY"]) as client:
      with client.sessions.create("python:3.12-slim",memory_mb=512,network_policy={"mode":"none"}) as box:
        messages=[{"role":"user","content":"Create /tmp/answer.py containing print(6 * 7), run it, and report its output."}]
        for _ in range(8):
          payload=json.dumps({"model":model,"messages":messages,"tools":tools,"stream":False}).encode()
          req=urllib.request.Request(url,data=payload,headers={"Content-Type":"application/json"})
          with urllib.request.urlopen(req,timeout=120) as response: message=json.load(response)["message"]
          messages.append(message);calls=message.get("tool_calls",[])
          if not calls: print(message.get("content",""));return
          for call in calls:
            fn=call["function"];args=fn.get("arguments",{})
            if isinstance(args,str):args=json.loads(args)
            messages.append({"role":"tool","content":dispatch_json(box,fn["name"],args)})
        raise SystemExit("model tool-call limit reached")
if __name__=="__main__":main()
