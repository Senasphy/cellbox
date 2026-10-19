export interface ExecResult {
  id: string; state: string; exit_code: number | null; signal: number | null;
  duration_ms: number; stdout: string; stderr: string; output_bytes: number; truncated: boolean;
}
export interface OutputChunk { stream: "stdout" | "stderr"; data: Uint8Array; text: string }
export interface SessionOptions {
  cpu_millicores?: number; memory_mb?: number; pids?: number; disk_mb?: number;
  ttl_s?: number; idle_ttl_s?: number; env?: Record<string,string>;
  labels?: Record<string,string>; network_policy?: Record<string,unknown>;
}
export class CellboxError extends Error { constructor(message: string) { super(message); this.name="CellboxError"; } }
export class ProblemError extends CellboxError {
  constructor(public status:number, public code:string, public title:string, detail:string) {
    super(detail || title); this.name="ProblemError";
  }
}
async function parse<T>(response:Response):Promise<T> {
  if (!response.ok) {
    let problem:any={}; try { problem=await response.json(); } catch {}
    throw new ProblemError(response.status, problem.code || "", problem.title || response.statusText, problem.detail || "");
  }
  if (response.status===204) return undefined as T;
  return await response.json() as T;
}
export class Client {
  readonly baseUrl:string;
  constructor(baseUrl="http://127.0.0.1:8080", readonly apiKey:string, private fetcher:typeof fetch=fetch) {
    if (!apiKey) throw new Error("apiKey is required"); this.baseUrl=baseUrl.replace(/\/$/,"");
  }
  async request<T>(method:string,path:string,body?:unknown):Promise<T> {
    const headers:Record<string,string>={Authorization:`Bearer ${this.apiKey}`};
    if (body!==undefined) { headers["Content-Type"]="application/json"; if(method==="POST") headers["Idempotency-Key"]=crypto.randomUUID(); }
    let last:unknown;
    for(let attempt=0;attempt<2;attempt++) try {
      return await parse<T>(await this.fetcher(this.baseUrl+path,{method,headers,body:body===undefined?undefined:JSON.stringify(body)}));
    } catch(e) { if(e instanceof ProblemError || attempt) throw e; last=e; }
    throw last;
  }
  sessions={
    create:async(image:string,options:SessionOptions={})=>{
      const {cpu_millicores,memory_mb,pids,disk_mb,ttl_s,idle_ttl_s,...rest}=options;
      const limits=Object.fromEntries(Object.entries({cpu_millicores,memory_mb,pids,disk_mb,ttl_s,idle_ttl_s}).filter(([,v])=>v!==undefined));
      const data=await this.request<any>("POST","/v1/sessions",{image,...rest,...(Object.keys(limits).length?{limits}:{})});
      return new Session(this,String(data.id),data);
    },
    get:async(id:string)=>new Session(this,id,await this.request<any>("GET",`/v1/sessions/${encodeURIComponent(id)}`)),
    list:async(limit=50)=>{const r=await this.request<any>("GET",`/v1/sessions?limit=${limit}`);return r.items||[];}
  };
}
export class Session {
  constructor(private client:Client,readonly id:string,readonly data:any={}){}
  async exec(command:string|string[],options:{timeout_s?:number;cwd?:string;env?:Record<string,string>}={}):Promise<ExecResult>{
    const body=commandBody(command,options); const r=await this.client.request<any>("POST",`/v1/sessions/${encodeURIComponent(this.id)}/execs?wait=true`,body);
    const stdout:string[]=[],stderr:string[]=[]; for(const o of r.output||[]) (o.stream==="stderr"?stderr:stdout).push(atob(o.data));
    const e=r.exec||{}; return {id:e.id||"",state:e.state||"",exit_code:e.exit_code??null,signal:e.signal??null,duration_ms:e.duration_ms||0,stdout:stdout.join(""),stderr:stderr.join(""),output_bytes:e.output_bytes||0,truncated:!!e.truncated};
  }
  async *execStream(command:string|string[],options:{timeout_s?:number;cwd?:string;env?:Record<string,string>}={}):AsyncIterable<OutputChunk>{
    const r=await this.client.request<any>("POST",`/v1/sessions/${encodeURIComponent(this.id)}/execs`,commandBody(command,options));
    const response=await fetch(this.client.baseUrl+r.stream_url,{headers:{Authorization:`Bearer ${this.client.apiKey}`}});
    if(!response.ok) await parse(response);
    const reader=response.body!.getReader(),decoder=new TextDecoder();let buf="",event="",data="";
    while(true){const {value,done}=await reader.read();buf+=decoder.decode(value||new Uint8Array(),{stream:!done});let i;
      while((i=buf.indexOf("\n"))>=0){const line=buf.slice(0,i).replace(/\r$/,"" );buf=buf.slice(i+1);
        if(!line){if(event==="output"&&data){const x=JSON.parse(data);const bytes=Uint8Array.from(atob(x.data),c=>c.charCodeAt(0));yield {stream:x.stream,data:bytes,text:new TextDecoder().decode(bytes)};}event="";data="";}
        else if(line.startsWith("event:"))event=line.slice(6).trim();else if(line.startsWith("data:"))data+=line.slice(5).trimStart();
      } if(done)break;
    }
  }
  async readFile(path:string):Promise<Uint8Array>{const r=await fetch(`${this.client.baseUrl}/v1/sessions/${encodeURIComponent(this.id)}/files?path=${encodeURIComponent(path)}`,{headers:{Authorization:`Bearer ${this.client.apiKey}`}});if(!r.ok)await parse(r);return new Uint8Array(await r.arrayBuffer());}
  async writeFile(path:string,contents:string|Uint8Array):Promise<void>{const r=await fetch(`${this.client.baseUrl}/v1/sessions/${encodeURIComponent(this.id)}/files?path=${encodeURIComponent(path)}`,{method:"PUT",headers:{Authorization:`Bearer ${this.client.apiKey}`,"Content-Type":"application/octet-stream"},body:typeof contents==="string"?contents:contents as BodyInit});await parse(r);}
  async listFiles(path="/",limit=50):Promise<any[]>{let cursor="",out:any[]=[];do{const q=new URLSearchParams({path,limit:String(limit),...(cursor?{cursor}:{})});const r=await this.client.request<any>("GET",`/v1/sessions/${encodeURIComponent(this.id)}/files/list?${q}`);out.push(...(r.items||[]));cursor=r.next_cursor||"";}while(cursor);return out;}
  async destroy():Promise<void>{await this.client.request("DELETE",`/v1/sessions/${encodeURIComponent(this.id)}`);}
  async using<T>(fn:(session:this)=>Promise<T>):Promise<T>{try{return await fn(this);}finally{await this.destroy();}}
}
function commandBody(command:string|string[],options:{timeout_s?:number;cwd?:string;env?:Record<string,string>}):any {
  const body:any=typeof command==="string"?{shell:command}:{argv:command};if(!body.shell&&!body.argv?.length)throw new Error("command must not be empty");
  const timeout=options.timeout_s??60;if(timeout<=0)throw new Error("timeout_s must be positive");body.timeout_ms=Math.round(timeout*1000);if(options.cwd)body.cwd=options.cwd;if(options.env)body.env=options.env;return body;
}
