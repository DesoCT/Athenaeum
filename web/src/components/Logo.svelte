<script lang="ts">
  /**
   * The Athenaeum mark: a temple facade (pediment, columns, steps) drawn in
   * currentColor so it inherits whatever text colour it sits in — the command
   * bar gives it var(--accent), which is theme-aware. The favicon and app icon
   * keep the fixed brand gold-on-ink; this in-app mark deliberately follows the
   * running theme instead.
   */
  interface Props {
    /** Rendered height; width follows the glyph's aspect ratio. */
    size?: string;
    /** An accessible name, or "" to mark the mark decorative (the default). */
    label?: string;
  }

  let { size = "1.1em", label = "" }: Props = $props();
</script>

<svg
  class="logo"
  viewBox="0 0 100 118"
  style="height: {size}"
  fill="currentColor"
  role={label ? "img" : "presentation"}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : "true"}
>
  <polygon points="50,0 0,26 100,26" />
  <rect x="36" y="36" width="8" height="62" />
  <rect x="56" y="36" width="8" height="62" />
  <rect x="22" y="50" width="56" height="8" />
  <rect x="22" y="74" width="56" height="8" />
  <rect x="16" y="104" width="68" height="6" />
  <rect x="8" y="112" width="84" height="6" />
</svg>

<style>
  .logo {
    width: auto;
    display: block;
    flex: none;
  }
</style>
