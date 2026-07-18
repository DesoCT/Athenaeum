/** Shapes returned by the Athenaeum API (spec 02 section 5). */

export interface Health {
  status: string;
  version: string;
  workspace: string;
  remote: boolean;
  frontend: "embedded" | "missing";
}

/** The stable error object every endpoint uses on failure. */
export interface ApiErrorBody {
  code: string;
  message: string;
  details?: Record<string, string>;
}
