// ViewEmpty is the shared empty and loading state for every renderer. A view
// with no rows says so; an empty frame would read as a broken surface, and the
// two are worth distinguishing at a glance.
export default function ViewEmpty(props: { message?: string }) {
  return (
    <p class="text-sm opacity-60 p-2" role="status">
      {props.message ?? "Nothing to show yet."}
    </p>
  );
}
