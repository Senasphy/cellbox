import { CellboxError, parseResponse } from "./errors.js";
import type { OutputChunk } from "./types.js";

interface SSEOutputPayload {
  stream: "stdout" | "stderr";
  data: string;
  seq?: number;
}

interface SSEErrorPayload {
  detail?: string;
  message?: string;
  title?: string;
}

export const utf8Decoder = new TextDecoder();

export function decodeBase64(encoded: string): Uint8Array {
  const binary = atob(encoded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

export async function* readExecEventStream(
  response: Response,
  execId: string,
): AsyncIterable<OutputChunk> {
  if (!response.ok) {
    await parseResponse(response);
  }

  const contentType = response.headers.get("content-type")?.toLowerCase() ?? "";
  if (!contentType.startsWith("text/event-stream")) {
    throw new CellboxError("exec stream returned an unexpected content type");
  }
  if (!response.body) {
    return;
  }

  const reader = response.body.getReader();
  const streamDecoder = new TextDecoder();
  let buffer = "";
  let eventName = "";
  let dataLines: string[] = [];
  let cursor = 0;

  while (true) {
    const { value, done } = await reader.read();
    buffer += streamDecoder.decode(value ?? new Uint8Array(), { stream: !done });

    let newlineIdx: number;
    while ((newlineIdx = buffer.indexOf("\n")) >= 0) {
      const line = buffer.slice(0, newlineIdx).replace(/\r$/, "");
      buffer = buffer.slice(newlineIdx + 1);

      if (line === "") {
        if (dataLines.length > 0) {
          const payload = dataLines.join("\n");
          if (eventName === "output") {
            yield parseOutputChunk(execId, payload, cursor);
          } else if (eventName === "error") {
            const err = JSON.parse(payload) as SSEErrorPayload;
            throw new CellboxError(err.detail || err.message || err.title || "exec stream failed");
          }
        }
        eventName = "";
        dataLines = [];
        cursor = 0;
        continue;
      }

      if (line.startsWith("event:")) {
        eventName = line.slice(6).trim();
      } else if (line.startsWith("data:")) {
        dataLines.push(line.slice(5).trimStart());
      } else if (line.startsWith("id:")) {
        cursor = Number(line.slice(3).trim());
        if (!Number.isFinite(cursor) || cursor < 0) {
          throw new CellboxError("invalid exec stream cursor");
        }
      }
    }

    if (done) {
      break;
    }
  }
}

function parseOutputChunk(execId: string, rawPayload: string, cursor: number): OutputChunk {
  const payload = JSON.parse(rawPayload) as SSEOutputPayload;
  const bytes = decodeBase64(payload.data);
  const sequence = Number(payload.seq ?? 0);
  return {
    stream: payload.stream,
    data: bytes,
    text: utf8Decoder.decode(bytes),
    sequence,
    cursor: cursor || sequence + bytes.length,
    execId,
  };
}
