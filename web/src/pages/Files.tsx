import { Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams } from "@solidjs/router";
import TreeList, { type FormState, type ListConfig, type ListCtx, type ListNode, type PageDescriptor } from "../components/TreeList";
import Button from "../components/Button";
import { useFormActions } from "../lib/formactions";
import { Download, Plus, Trash } from "../components/icons";
import {
  type FileRow,
  FILES_KEY,
  listFiles,
  createFile,
  downloadFile,
  deleteFile,
  humanSize,
  dataUrlToBase64,
} from "../lib/files";
import { useMe, can } from "../lib/auth";
import { describeError, fmtTime } from "../lib/format";

// Files: the tenant-wide file directory, built on the generic TreeList in flat
// mode (files have no tree, no parent, no scope). A file is a content-addressed
// blob with a searchable handle: name, MIME type, size, hash, and a sensitivity
// flag that hides it from non-admins. The row is addressed by its id (names are
// not unique across files), so the deep-link route is /files/:id. Create is an
// upload (a native file input, base64-encoded client-side); the detail offers
// Download and Delete. There is no in-place edit in this slice.
type FileNode = ListNode & {
  raw: FileRow;
  content_type: string;
  size: number;
  sensitive: boolean;
  created_at: string;
};

// The static config (matrix-tested in pages/descriptors.test.ts); the page spreads
// it into its ListConfig and adds the live wiring. `name` is TreeList's implicit
// first column (node.display), so it is not listed here.
export const filesDescriptor: PageDescriptor = {
  entity: { name: "file", plural: "Files" },
  storageKey: "og-file",
  columns: {
    content_type: { label: "Type", width: 200 },
    size: { label: "Size", width: 110 },
    sensitive: { label: "Sensitive", width: 120 },
    created_at: { label: "Added", width: 170 },
  },
  columnKeys: ["content_type", "size", "sensitive", "created_at"],
  defaultCols: ["content_type", "size", "sensitive", "created_at"],
};

export default function Files() {
  const params = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();

  const files = useQuery(() => ({ queryKey: FILES_KEY, queryFn: listFiles }));

  // Flat forest: each file is a childless node addressed by its id, sorted by name.
  const nodes = createMemo<FileNode[]>(() =>
    [...(files.data ?? [])]
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((f) => ({
        id: f.id,
        display: f.name,
        children: [],
        raw: f,
        content_type: f.content_type,
        size: f.size,
        sensitive: f.sensitive,
        created_at: f.created_at,
      })),
  );

  const [err, setErr] = createSignal<string | null>(null);

  async function del(n: FileNode) {
    if (!confirm(`Delete file "${n.raw.name}"?`)) return;
    setErr(null);
    try {
      await deleteFile(n.raw.id);
      await qc.invalidateQueries({ queryKey: FILES_KEY });
      navigate("/files");
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function download(n: FileNode) {
    setErr(null);
    const dl = await downloadFile(n.raw.id);
    triggerBrowserDownload(dl.name, dl.content_type, dl.content);
  }

  // FileDetail: the read-only file card, shared by the blade and the full page. It
  // shows the handle's metadata and the Download action; on the full page it also
  // renders its own Delete footer (a blade gets Delete from the BladeStack footer).
  function FileDetail(props: { node: FileNode; ctx: ListCtx<FileNode> }): JSX.Element {
    const ctx = props.ctx;
    // Re-resolve from the live index so a background refetch updates the facts.
    const n = () => ctx.byId(props.node.id) ?? props.node;
    const [busy, setBusy] = createSignal(false);
    const [dlErr, setDlErr] = createSignal<string | null>(null);

    async function doDownload() {
      setBusy(true);
      setDlErr(null);
      try {
        await download(n());
      } catch (e) {
        setDlErr(describeError(e));
      } finally {
        setBusy(false);
      }
    }

    return (
      <div class="flex flex-col gap-5">
        <Show when={dlErr()}>
          <div role="alert" class="alert alert-error alert-soft text-sm"><span>{dlErr()}</span></div>
        </Show>
        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">File</span>
          <div class="grid grid-cols-2 gap-5">
            {ctx.fact("Name", <span class="font-data text-sm break-all">{n().raw.name}</span>)}
            {ctx.fact("Type", <span class="badge badge-ghost badge-sm">{n().content_type}</span>)}
            {ctx.fact("Size", <span class="text-sm">{humanSize(n().size)}</span>)}
            {ctx.fact(
              "Sensitivity",
              n().sensitive
                ? <span class="badge badge-warning badge-sm">Sensitive</span>
                : <span class="text-sm text-base-content/50">Normal</span>,
            )}
            {ctx.fact("Added", <span class="text-sm">{fmtTime(n().created_at)}</span>)}
            {ctx.fact("ID", <span class="font-data text-xs text-base-content/50">{n().raw.id}</span>)}
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Content hash</span>
          <span class="font-data text-xs break-all text-base-content/60">{n().raw.sha256}</span>
        </div>

        <div class="flex flex-wrap items-center gap-2 border-t border-base-300 pt-4">
          <Button intent="action" icon={Download} loading={busy()} onClick={() => void doDownload()}>Download</Button>
          <Show when={ctx.full && can(me.data, "file", "delete")}>
            <span class="flex-1" />
            <Button intent="danger" icon={Trash} onClick={() => del(n())}>Delete</Button>
          </Show>
        </div>
      </div>
    );
  }

  const cfg: ListConfig<FileNode> = {
    ...filesDescriptor,
    flat: true,
    nodes,
    focus: () => params.id,
    loading: () => files.isLoading,
    error: () => files.error,
    filterPlaceholder: "Filter by name, type…",
    cellFor: (key, n) => {
      if (key === "content_type") return <span class="badge badge-ghost badge-sm">{n.content_type}</span>;
      if (key === "size") return <span class="text-base-content/70">{humanSize(n.size)}</span>;
      if (key === "sensitive") return n.sensitive ? <span class="badge badge-warning badge-sm">Sensitive</span> : <span class="text-base-content/40">—</span>;
      if (key === "created_at") return <span class="text-base-content/70">{fmtTime(n.created_at)}</span>;
      return null;
    },
    filterKeys: () => [
      { key: "name", type: "string", hint: "substring", get: (n) => `${n.display} ${n.content_type}`, values: () => [] },
      { key: "type", type: "string", hint: "exact", get: (n) => n.content_type, values: (rows) => [...new Set(rows.map((r) => r.content_type))].sort() },
      { key: "sensitive", type: "string", hint: "exact", get: (n) => (n.sensitive ? "sensitive" : "normal"), values: () => ["sensitive", "normal"] },
    ],
    sortVal: (n, key) => {
      if (key === "content_type") return n.content_type.toLowerCase();
      if (key === "size") return n.size;
      if (key === "sensitive") return String(n.sensitive);
      if (key === "created_at") return n.created_at;
      return n.display.toLowerCase();
    },
    FormBody: FileForm,
    onOpenNode: (n) => navigate(`/files/${encodeURIComponent(n.id)}`),
    onBack: () => navigate("/files"),
    onDelete: (n) => del(n),
    renderDetail: (n, ctx) => <FileDetail node={n} ctx={ctx} />,
  };

  // No page H1: inventory pages built on TreeList let the top bar label them, and
  // the full-page detail renders its own heading (see Page.tsx).
  return (
    <div class="og-stack flex flex-col">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <TreeList config={cfg} />
    </div>
  );
}

// FileForm: the create Drawer body. Create is an upload: pick a file, read its
// bytes, base64-encode them, and default the name and MIME type from the picked
// File. Files carry no in-place edit in this slice, so the edit branch is a note.
function FileForm(props: { form: FormState<FileNode>; close: () => void; ctx: ListCtx<FileNode> }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const [name, setName] = createSignal("");
  const [contentType, setContentType] = createSignal("");
  const [content, setContent] = createSignal<string | null>(null); // base64
  const [pickedName, setPickedName] = createSignal<string | null>(null);
  const [sensitive, setSensitive] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);
  // Setting a file sensitive needs the admin tier (file:create:admin); hide the
  // toggle otherwise so the operator is not offered a choice the server rejects.
  const canSetSensitive = () => can(me.data, "file", "create", "admin");

  // Bound by the create body, not here: a file has no edit path, so the shell must
  // not draw an Upload bar over the cannot-be-edited note. The binding mounts and
  // unmounts with the form it belongs to.
  const UploadAction = () => {
    useFormActions().bind({
      submitLabel: "Upload file",
      submitIcon: Plus,
      submit: () => void submit(),
      busy,
      disabled: () => content() === null,
    });
    return null;
  };

  function onPick(input: HTMLInputElement) {
    const f = input.files?.[0];
    if (!f) {
      setContent(null);
      setPickedName(null);
      return;
    }
    setPickedName(f.name);
    // Default the name from the filename only when the operator has not typed one.
    setName((cur) => (cur.trim() ? cur : f.name));
    setContentType(f.type || "application/octet-stream");
    const reader = new FileReader();
    reader.onload = () => {
      setContent(dataUrlToBase64(String(reader.result ?? "")));
      setFormErr(null);
    };
    reader.onerror = () => setFormErr("Could not read that file.");
    reader.readAsDataURL(f);
  }

  async function submit() {
    const c = content();
    if (c === null) {
      setFormErr("Choose a file to upload.");
      return;
    }
    setBusy(true);
    setFormErr(null);
    try {
      await createFile({
        name: name().trim() || pickedName() || "file",
        contentType: contentType() || "application/octet-stream",
        content: c,
        sensitive: sensitive(),
      });
      await qc.invalidateQueries({ queryKey: FILES_KEY });
      props.close();
    } catch (er) {
      setFormErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Show
      when={props.form.mode === "create"}
      fallback={<p class="text-sm text-base-content/50">Files cannot be edited. Delete and re-upload to replace one.</p>}
    >
      <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
        <UploadAction />
        <Show when={formErr()}>
          <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
        </Show>
        <Field label="File" hint="The bytes to store. Identical bytes are deduplicated to one blob.">
          <input type="file" class="file-input file-input-bordered w-full" onChange={(e) => onPick(e.currentTarget)} />
        </Field>
        <Field label="Name" hint="A label for the file; defaults to the uploaded filename.">
          <input class="input input-bordered w-full font-data" value={name()} placeholder="firmware-2.1.bin" onInput={(e) => setName(e.currentTarget.value)} />
        </Field>
        <Field label="Content type" hint="The MIME type used to serve the file.">
          <input class="input input-bordered w-full font-data" value={contentType()} placeholder="application/octet-stream" onInput={(e) => setContentType(e.currentTarget.value)} />
        </Field>
        <Show when={canSetSensitive()}>
          <label class="flex items-center gap-2 text-sm">
            <input type="checkbox" class="checkbox checkbox-sm" checked={sensitive()} onChange={(e) => setSensitive(e.currentTarget.checked)} />
            <span>Sensitive (only the admin tier can see or download it)</span>
          </label>
        </Show>
      </form>
    </Show>
  );
}

// A labelled field for the create Drawer. The label wraps its control (a native
// input, no interactive trigger inside), so the association is standard HTML.
function Field(p: { label: string; hint?: string; children: JSX.Element }): JSX.Element {
  return (
    <label class="flex flex-col gap-1">
      <span class="text-[12px] font-medium text-base-content/70">{p.label}</span>
      {p.children}
      <Show when={p.hint}><span class="text-[11px] text-base-content/40">{p.hint}</span></Show>
    </label>
  );
}

// triggerBrowserDownload turns a base64 blob into a file the browser saves: decode
// to bytes, wrap in a Blob under the returned MIME type, and click a transient
// anchor, revoking the object URL after.
function triggerBrowserDownload(name: string, contentType: string, base64: string) {
  const bin = atob(base64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  const blob = new Blob([bytes], { type: contentType || "application/octet-stream" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}
