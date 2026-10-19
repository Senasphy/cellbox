# TypeScript SDK

`npm install && npm run build` emits ESM and CommonJS bundles plus declarations. The fetch based client supports session creation, retrieval and listing, file operations, command execution, SSE output as an `AsyncIterable`, and explicit session destruction. Use `session.using(async box => ...)` to guarantee destruction in a `finally` block.

```ts
import { Client } from "@cellbox/sdk";
const client = new Client("http://127.0.0.1:8080", process.env.CELLBOX_KEY!);
const box = await client.sessions.create("alpine:latest", { memory_mb: 512 });
await box.using(async session => {
  await session.writeFile("/tmp/hello", "hello\n");
  console.log((await session.exec(["cat", "/tmp/hello"])).stdout);
});
```
