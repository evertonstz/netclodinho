import { useMemo, ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { oneDark, oneLight } from "react-syntax-highlighter/dist/esm/styles/prism";
import { useMantineColorScheme, Box, Code, Text, Anchor } from "@mantine/core";
import type { Components } from "react-markdown";

interface StreamingMarkdownProps {
  content: string;
  isStreaming?: boolean;
}

export function StreamingMarkdown({ content, isStreaming }: StreamingMarkdownProps) {
  const { colorScheme } = useMantineColorScheme();
  const isDark = colorScheme === "dark";

  // Detect incomplete code blocks during streaming
  const processedContent = useMemo(() => {
    if (!isStreaming) return content;

    // Count backticks to detect incomplete code blocks
    const tripleBackticks = (content.match(/```/g) || []).length;
    if (tripleBackticks % 2 !== 0) {
      // Odd number means unclosed code block - close it
      return content + "\n```";
    }
    return content;
  }, [content, isStreaming]);

  const components: Components = useMemo(
    () => ({
      code({ className, children }: { className?: string; children?: ReactNode }) {
        const match = /language-(\w+)/.exec(className || "");
        const isInline = !match && !className;

        if (isInline) {
          return (
            <Code style={{ fontSize: 13 }}>
              {children}
            </Code>
          );
        }

        const language = match ? match[1] : "text";
        const codeString = String(children).replace(/\n$/, "");

        return (
          <Box
            style={{
              borderRadius: 8,
              overflow: "hidden",
              margin: "8px 0",
              position: "relative",
            }}
          >
            {language !== "text" && (
              <Box
                style={{
                  position: "absolute",
                  top: 4,
                  right: 8,
                  fontSize: 10,
                  opacity: 0.6,
                  textTransform: "uppercase",
                  letterSpacing: 0.5,
                }}
              >
                {language}
              </Box>
            )}
            <SyntaxHighlighter
              style={isDark ? oneDark : oneLight}
              language={language}
              PreTag="div"
              customStyle={{
                margin: 0,
                padding: "12px",
                fontSize: 13,
                lineHeight: 1.5,
              }}
            >
              {codeString}
            </SyntaxHighlighter>
          </Box>
        );
      },
      p({ children }: { children?: ReactNode }) {
        return (
          <Text component="p" size="sm" style={{ margin: "8px 0", lineHeight: 1.6 }}>
            {children}
          </Text>
        );
      },
      a({ href, children }: { href?: string; children?: ReactNode }) {
        return (
          <Anchor href={href} target="_blank" rel="noopener noreferrer" size="sm">
            {children}
          </Anchor>
        );
      },
      ul({ children }: { children?: ReactNode }) {
        return (
          <Box component="ul" style={{ margin: "8px 0", paddingLeft: 20 }}>
            {children}
          </Box>
        );
      },
      ol({ children }: { children?: ReactNode }) {
        return (
          <Box component="ol" style={{ margin: "8px 0", paddingLeft: 20 }}>
            {children}
          </Box>
        );
      },
      li({ children }: { children?: ReactNode }) {
        return (
          <Text component="li" size="sm" style={{ marginBottom: 4, lineHeight: 1.6 }}>
            {children}
          </Text>
        );
      },
      blockquote({ children }: { children?: ReactNode }) {
        return (
          <Box
            component="blockquote"
            style={{
              margin: "8px 0",
              paddingLeft: 12,
              borderLeft: "3px solid var(--mantine-color-dimmed)",
              opacity: 0.85,
            }}
          >
            {children}
          </Box>
        );
      },
      h1({ children }: { children?: ReactNode }) {
        return <Text size="xl" fw={600} mt="md" mb="xs">{children}</Text>;
      },
      h2({ children }: { children?: ReactNode }) {
        return <Text size="lg" fw={600} mt="md" mb="xs">{children}</Text>;
      },
      h3({ children }: { children?: ReactNode }) {
        return <Text size="md" fw={600} mt="sm" mb="xs">{children}</Text>;
      },
    }),
    [isDark]
  );

  return (
    <Box className="streaming-markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {processedContent}
      </ReactMarkdown>
    </Box>
  );
}
