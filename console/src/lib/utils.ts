import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * shadcn / ai-elements canonical class merger. clsx normalizes a mixed
 * argument list (strings, conditionals, arrays, objects); twMerge
 * resolves conflicting Tailwind utilities so the *last* one wins
 * (e.g., `cn("p-2", "p-4")` → `"p-4"`).
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/**
 * Copy text to the clipboard. Uses the Clipboard API when available
 * (HTTPS / localhost), and falls back to a temporary textarea +
 * document.execCommand("copy") for insecure HTTP origins.
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall through to execCommand for non-secure contexts / denied permission.
    }
  }

  if (typeof document === "undefined") {
    return false;
  }

  try {
    const textarea = document.createElement("textarea");
    textarea.value = text;
    textarea.setAttribute("readonly", "");
    textarea.style.position = "fixed";
    textarea.style.left = "-9999px";
    textarea.style.top = "0";
    document.body.appendChild(textarea);
    textarea.focus();
    textarea.select();
    textarea.setSelectionRange(0, text.length);
    const ok = document.execCommand("copy");
    document.body.removeChild(textarea);
    return ok;
  } catch {
    return false;
  }
}
