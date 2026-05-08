<script lang="ts">
  import {
    type Clause,
    type ClauseOp,
    parseClauses,
    clausesToExpr,
    OP_LABELS,
    defaultOpsForField,
  } from "./filter-dsl";

  let {
    expression,
    discoveredFields,
    onApply,
  } = $props<{
    expression: string;
    discoveredFields: Set<string>;
    onApply: (expr: string) => void;
  }>();

  // Built-in fields the server treats specially or always exposes.
  const BUILTINS = ["level", "msg", "source", "service", "ts"];

  let hotColumns = $state<string[]>([]);

  $effect(() => {
    fetch("/api/columns")
      .then((r) => (r.ok ? r.json() : { hot: [] }))
      .then((j) => (hotColumns = (j.hot ?? []) as string[]))
      .catch(() => {});
  });

  let columnOptions = $derived(() => {
    const set = new Set<string>(BUILTINS);
    for (const c of hotColumns) set.add(c);
    for (const p of discoveredFields) set.add(p);
    return [...set].sort((a, b) => {
      const ai = BUILTINS.indexOf(a);
      const bi = BUILTINS.indexOf(b);
      if (ai !== -1 || bi !== -1) {
        return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi);
      }
      return a.localeCompare(b);
    });
  });

  // Parsed view of the current expression. When it doesn't decompose, we
  // hide the chips and show "Advanced — edit raw" instead.
  let parsed = $derived(parseClauses(expression));

  let showAdd = $state(false);
  let newField = $state("level");
  let newOp = $state<ClauseOp>("eq");
  let newValue = $state("");
  let newRangeHi = $state("");

  function commit(clauses: Clause[]) {
    onApply(clausesToExpr(clauses));
  }

  function removeAt(i: number) {
    const next = parsed.clauses.filter((_, j) => j !== i);
    commit(next);
  }

  function toggleNegate(i: number) {
    const c = parsed.clauses[i];
    if (!c) return;
    const flips: Partial<Record<ClauseOp, ClauseOp>> = {
      eq: "neq",
      neq: "eq",
      contains: "ncontains",
      ncontains: "contains",
    };
    const nextOp = flips[c.op];
    if (!nextOp) return;
    const next = [...parsed.clauses];
    next[i] = { ...c, op: nextOp };
    commit(next);
  }

  function addClause() {
    const v = newValue.trim();
    if (newOp !== "range" && v === "") return;
    let value = v;
    if (newOp === "range") {
      const hi = newRangeHi.trim();
      if (!v || !hi) return;
      value = `${v}..${hi}`;
    }
    const clause: Clause = { field: newField, op: newOp, value };
    commit([...parsed.clauses, clause]);
    newValue = "";
    newRangeHi = "";
    showAdd = false;
  }

  $effect(() => {
    // Reset op if it's not legal for the current field.
    const ops = defaultOpsForField(newField);
    if (!ops.includes(newOp)) newOp = ops[0]!;
  });
</script>

<div class="text-sm">
  <div class="flex items-center justify-between mb-2">
    <h2 class="font-semibold">Filters</h2>
    <button
      class="text-xs px-2 py-0.5 rounded bg-sky-600 text-white hover:bg-sky-700"
      title="Add a filter clause"
      onclick={() => (showAdd = !showAdd)}>+</button>
  </div>

  {#if parsed.advanced}
    <p class="text-[11px] text-zinc-500 mb-2">
      Advanced expression — edit the filter input above to use chips here.
    </p>
  {:else if parsed.clauses.length === 0}
    <p class="text-[11px] text-zinc-500 mb-2">No clauses. Click + to add one.</p>
  {:else}
    <div class="flex flex-wrap gap-1 mb-2">
      {#each parsed.clauses as c, i}
        <span
          class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-zinc-200 dark:bg-zinc-800 mono text-[11px]">
          {#if c.op === "neq" || c.op === "ncontains"}
            <span class="text-red-500" title="negated">!</span>
          {/if}
          <button
            class="hover:underline"
            title="toggle negate"
            onclick={() => toggleNegate(i)}>{c.field}</button>
          <span class="text-zinc-500">{OP_LABELS[c.op]}</span>
          <span title={c.value} class="truncate max-w-[120px]">{c.value}</span>
          <button
            class="text-zinc-500 hover:text-red-500 ml-0.5"
            title="remove"
            onclick={() => removeAt(i)}>×</button>
        </span>
      {/each}
    </div>
  {/if}

  {#if showAdd}
    <div class="rounded bg-zinc-100 dark:bg-zinc-900 p-2 mb-2 space-y-1.5">
      <select
        class="w-full px-1.5 py-1 rounded bg-white dark:bg-zinc-800 mono text-[11px]"
        bind:value={newField}>
        {#each columnOptions() as col}
          <option value={col}>{col}</option>
        {/each}
      </select>
      <div class="flex gap-1">
        <select
          class="px-1.5 py-1 rounded bg-white dark:bg-zinc-800 text-[11px]"
          bind:value={newOp}>
          {#each defaultOpsForField(newField) as op}
            <option value={op}>{OP_LABELS[op]}</option>
          {/each}
        </select>
        <input
          class="flex-1 px-1.5 py-1 rounded bg-white dark:bg-zinc-800 mono text-[11px]"
          placeholder={newOp === "range" ? "lo" : "value"}
          bind:value={newValue}
          onkeydown={(e) => e.key === "Enter" && addClause()} />
        {#if newOp === "range"}
          <input
            class="w-16 px-1.5 py-1 rounded bg-white dark:bg-zinc-800 mono text-[11px]"
            placeholder="hi"
            bind:value={newRangeHi}
            onkeydown={(e) => e.key === "Enter" && addClause()} />
        {/if}
      </div>
      <div class="flex justify-end gap-1">
        <button
          class="text-[11px] px-2 py-0.5 rounded bg-zinc-200 dark:bg-zinc-700"
          onclick={() => (showAdd = false)}>cancel</button>
        <button
          class="text-[11px] px-2 py-0.5 rounded bg-sky-600 text-white hover:bg-sky-700"
          onclick={addClause}>add</button>
      </div>
    </div>
  {/if}
</div>
