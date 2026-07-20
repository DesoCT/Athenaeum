/**
 * markdown-it-task-lists ships no types and has no @types package, so the
 * minimal surface Athenaeum uses is declared here rather than falling back to
 * an implicit `any`.
 */
declare module "markdown-it-task-lists" {
  import type { PluginWithOptions } from "markdown-it";

  interface TaskListsOptions {
    /** Render checkboxes as interactive. Athenaeum always passes false. */
    enabled?: boolean;
    /** Wrap the item text in a <label>. */
    label?: boolean;
    labelAfter?: boolean;
  }

  const taskLists: PluginWithOptions<TaskListsOptions>;
  export default taskLists;
}
