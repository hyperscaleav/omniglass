// ViewError is what a renderer shows when the view cannot be rendered: the
// server refused or failed it, or the result no longer matches the
// field-mapping the renderer was given.
//
// It exists because the alternative is worse than ugly. Without it a failed
// view sits on "Loading..." forever, telling an operator the console is still
// working on a request the server has already refused, and a broken mapping
// throws out of the render and takes the whole page with it. A widget that
// fails should fail visibly and alone.
export default function ViewError(props: { message: string }) {
  return (
    <p class="text-sm text-error p-2" role="alert">
      {props.message}
    </p>
  );
}

// describeViewError renders any thrown or returned failure as one line. An
// Error keeps its message (the field-mapping errors say exactly which column
// is missing); anything else is stringified rather than swallowed.
export function describeViewError(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return "this view could not be loaded";
}
