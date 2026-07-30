<script lang="ts">
  import { settings, persistSettings, type DefaultView, type AnnotateTrigger } from "./settings.svelte";

  let open = $state(false);

  function setView(v: DefaultView): void {
    settings.defaultView = v;
    persistSettings();
  }
  function setAnnotate(a: AnnotateTrigger): void {
    settings.annotateOn = a;
    persistSettings();
  }
  function toggleWrap(): void {
    settings.wrapLines = !settings.wrapLines;
    persistSettings();
  }

  const views: { value: DefaultView; label: string }[] = [
    { value: "split", label: "Split" },
    { value: "source", label: "Source" },
    { value: "preview", label: "Preview" },
  ];
  const triggers: { value: AnnotateTrigger; label: string; hint: string }[] = [
    { value: "button", label: "Show a button", hint: "A small Comment button by the selection" },
    { value: "popover", label: "Open immediately", hint: "The comment form opens on selection" },
    { value: "off", label: "Off", hint: "Selecting never starts a comment" },
  ];
</script>

<div class="settings">
  <button
    type="button"
    class="trigger"
    aria-haspopup="menu"
    aria-expanded={open}
    aria-label="Settings"
    title="Settings"
    onclick={() => (open = !open)}
  >
    <span class="gear" aria-hidden="true">⚙</span>
  </button>

  {#if open}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <button type="button" class="backdrop" aria-label="Close settings" onclick={() => (open = false)}></button>

    <div class="panel" role="menu">
      <p class="panel-title">Settings</p>
      <p class="panel-note">These preferences are saved in this browser.</p>

      <fieldset class="group">
        <legend>Default view</legend>
        <div class="segmented" role="group">
          {#each views as v}
            <button type="button" class:active={settings.defaultView === v.value} onclick={() => setView(v.value)}>
              {v.label}
            </button>
          {/each}
        </div>
        <p class="group-hint">Which mode a document opens in.</p>
      </fieldset>

      <fieldset class="group">
        <legend>Starting a comment</legend>
        {#each triggers as t}
          <label class="radio">
            <input
              type="radio"
              name="annotate-on"
              value={t.value}
              checked={settings.annotateOn === t.value}
              onchange={() => setAnnotate(t.value)}
            />
            <span class="radio-body">
              <span class="radio-label">{t.label}</span>
              <span class="radio-hint">{t.hint}</span>
            </span>
          </label>
        {/each}
      </fieldset>

      <fieldset class="group">
        <label class="check">
          <input type="checkbox" checked={settings.wrapLines} onchange={toggleWrap} />
          <span>Wrap long lines in the editor by default</span>
        </label>
      </fieldset>
    </div>
  {/if}
</div>

<svelte:window onkeydown={(e) => e.key === "Escape" && (open = false)} />

<style>
  .settings { position: relative; }
  .trigger {
    display: flex;
    align-items: center;
    padding: 0.25rem 0.55rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    cursor: pointer;
  }
  .trigger:hover { border-color: var(--focus); color: var(--text-primary); }
  .gear { font-size: 0.95rem; line-height: 1; }
  .backdrop { position: fixed; inset: 0; z-index: 20; border: 0; background: none; cursor: default; }
  .panel {
    position: absolute;
    z-index: 21;
    top: calc(100% + 0.35rem);
    right: 0;
    width: 19rem;
    max-height: 80vh;
    overflow-y: auto;
    padding: 0.6rem 0.75rem 0.75rem;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--surface-raised);
    box-shadow: 0 6px 24px rgb(0 0 0 / 35%);
  }
  .panel-title { margin: 0; font-size: 0.85rem; font-weight: 600; color: var(--text-primary); }
  .panel-note { margin: 0.1rem 0 0.6rem; font-size: 0.72rem; color: var(--text-muted); }
  .group { margin: 0 0 0.9rem; padding: 0; border: 0; }
  .group legend {
    padding: 0; margin-bottom: 0.4rem; font-size: 0.68rem; font-weight: 600;
    letter-spacing: 0.06em; text-transform: uppercase; color: var(--text-secondary);
  }
  .group-hint { margin: 0.35rem 0 0; font-size: 0.72rem; color: var(--text-muted); }
  .segmented { display: flex; border: 1px solid var(--line-strong); border-radius: var(--radius); overflow: hidden; }
  .segmented button {
    flex: 1; padding: 0.3rem 0.4rem; border: 0; border-right: 1px solid var(--line-strong);
    background: var(--surface-panel); color: var(--text-secondary); font: inherit; font-size: 0.75rem; cursor: pointer;
  }
  .segmented button:last-child { border-right: 0; }
  .segmented button.active { background: var(--surface-raised); color: var(--accent); }
  .radio { display: flex; align-items: flex-start; gap: 0.5rem; padding: 0.3rem 0; cursor: pointer; }
  .radio input { margin-top: 0.2rem; }
  .radio-body { display: flex; flex-direction: column; }
  .radio-label { font-size: 0.82rem; color: var(--text-primary); }
  .radio-hint { font-size: 0.72rem; color: var(--text-muted); }
  .check { display: flex; align-items: flex-start; gap: 0.5rem; font-size: 0.82rem; color: var(--text-primary); cursor: pointer; }
</style>
