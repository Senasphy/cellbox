interface ProblemBody {
  code?: string;
  title?: string;
  detail?: string;
}

export class CellboxError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "CellboxError";
  }
}

export class ProblemError extends CellboxError {
  constructor(
    public status: number,
    public code: string,
    public title: string,
    public detail: string,
  ) {
    super(detail || title);
    this.name = "ProblemError";
  }
}

export async function parseResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let problem: ProblemBody = {};
    try {
      problem = (await response.json()) as ProblemBody;
    } catch {
      // Ignore non-JSON error bodies and fall back to HTTP status text.
    }
    throw new ProblemError(
      response.status,
      problem.code ?? "",
      problem.title ?? response.statusText,
      problem.detail ?? "",
    );
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}
