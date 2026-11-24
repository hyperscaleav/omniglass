import { isDenied } from "../api/client";

// queryRetry is the app's retry policy for a read: an ordinary failure is retried
// a few times (a blip, a restart), but a refusal (403) never is. Retrying a "no"
// only holds the page in its pending state through the whole backoff, which is how
// a page a person may not read looked like it was loading forever (#2442).
export function queryRetry(failureCount: number, error: unknown): boolean {
  return !isDenied(error) && failureCount < 3;
}
