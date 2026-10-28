/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      colors: {
        // Primary ink system (Airtable-inspired)
        ink: "#181d26",
        "ink-active": "#0d1218",
        body: "#333840",
        muted: "#41454d",

        // Surfaces
        canvas: "#ffffff",
        "surface-soft": "#f8fafc",
        "surface-strong": "#e0e2e6",
        "surface-dark": "#181d26",

        // Borders
        hairline: "#dddddd",
        "border-strong": "#9297a0",

        // Semantic
        link: "#1b61c9",
        "link-active": "#1a3866",
        info: "#254fad",
        "info-border": "#458fff",
        success: "#006400",
        "success-border": "#39bf45",
        warning: "#d97706",
        "warning-bg": "#fef3c7",
        "warning-text": "#92400e",
        error: "#dc2626",
        "error-bg": "#fef2f2",
        "error-text": "#991b1b",

        // Status colors
        "status-paid": "#16a34a",
        "status-pending": "#ca8a04",
        "status-failed": "#dc2626",
        "status-expired": "#6b7280",
      },
      fontFamily: {
        sans: [
          "Inter",
          "-apple-system",
          "BlinkMacSystemFont",
          "Segoe UI",
          "Roboto",
          "sans-serif",
        ],
        mono: ["JetBrains Mono", "Fira Code", "monospace"],
      },
      fontSize: {
        // Display sizes
        "display-xl": ["48px", { lineHeight: "1.1", fontWeight: "500" }],
        "display-lg": ["40px", { lineHeight: "1.2", fontWeight: "400" }],
        "display-md": ["32px", { lineHeight: "1.2", fontWeight: "400" }],
        // Title sizes
        "title-lg": ["24px", { lineHeight: "1.35", fontWeight: "400" }],
        "title-md": ["20px", { lineHeight: "1.5", fontWeight: "400" }],
        "title-sm": ["18px", { lineHeight: "1.4", fontWeight: "500" }],
        // Label
        "label-md": ["16px", { lineHeight: "1.4", fontWeight: "500" }],
        // Body
        "body-md": ["14px", { lineHeight: "1.5", fontWeight: "400" }],
        "body-sm": ["13px", { lineHeight: "1.5", fontWeight: "400" }],
        // Caption
        caption: ["13px", { lineHeight: "1.4", fontWeight: "500" }],
      },
      borderRadius: {
        xs: "2px",
        sm: "6px",
        md: "10px",
        lg: "12px",
        pill: "9999px",
      },
      spacing: {
        xxs: "4px",
        xs: "8px",
        sm: "12px",
        md: "16px",
        lg: "24px",
        xl: "32px",
        xxl: "48px",
        section: "96px",
      },
      boxShadow: {
        button: "0 1px 2px rgba(0, 0, 0, 0.05)",
        "button-focus": "0 0 0 2px rgba(27, 97, 201, 0.3)",
        card: "0 1px 3px rgba(0, 0, 0, 0.04)",
      },
      transitionTimingFunction: {
        smooth: "cubic-bezier(0.16, 1, 0.3, 1)",
      },
    },
  },
  plugins: [],
};