import { useEffect, useRef, useState, useCallback, useId } from 'react';
import { cn } from '../lib/utils';

// Track registered languages to avoid re-registering
const registeredLanguages = new Set<string>();

/**
 * Props for CelEditor component
 */
export interface CelEditorProps {
  /** Current value of the editor */
  value: string;
  /** Callback when value changes */
  onChange: (value: string) => void;
  /** Language mode: 'cel' for expressions, 'cel-template' for {{ }} templates */
  language?: 'cel' | 'cel-template';
  /** Field paths available for autocomplete */
  availableFields?: string[];
  /** Placeholder text when empty */
  placeholder?: string;
  /** Height of the editor */
  height?: string | number;
  /** Additional CSS classes */
  className?: string;
  /** Show error border */
  error?: boolean;
  /** Whether editor is read-only */
  readOnly?: boolean;
  /** Test ID for E2E testing */
  'data-testid'?: string;
}

// Types for Monaco (loaded dynamically)
type Monaco = typeof import('monaco-editor');
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type MonacoEditorComponent = any;

/**
 * Lazy-loaded Monaco editor for CEL expressions and templates
 * Provides autocomplete based on available field paths
 */
export function CelEditor({
  value,
  onChange,
  language = 'cel',
  availableFields = [],
  height = '100px',
  className,
  error = false,
  readOnly = false,
  'data-testid': testId,
}: CelEditorProps) {
  const [Editor, setEditor] = useState<MonacoEditorComponent | null>(null);
  const [loadError, setLoadError] = useState<Error | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const monacoRef = useRef<Monaco | null>(null);
  const disposableRef = useRef<{ dispose: () => void } | null>(null);

  // Generate a unique language ID for this editor instance
  const instanceId = useId();
  const languageId = `cel-${instanceId.replace(/:/g, '-')}`;

  // Load Monaco Editor component
  useEffect(() => {
    let mounted = true;
    let timeoutId: ReturnType<typeof setTimeout> | null = null;

    async function loadMonaco() {
      try {
        // Set a timeout for the entire load process
        timeoutId = setTimeout(() => {
          if (mounted && isLoading) {
            setLoadError(new Error('Monaco loading timeout'));
            setIsLoading(false);
          }
        }, 5000);

        // Dynamic import of @monaco-editor/react
        const { default: MonacoEditorComponent } = await import('@monaco-editor/react');

        if (!mounted) return;

        if (timeoutId) {
          clearTimeout(timeoutId);
        }

        setEditor(() => MonacoEditorComponent);
        setIsLoading(false);
      } catch (err) {
        if (mounted) {
          if (timeoutId) {
            clearTimeout(timeoutId);
          }
          setLoadError(err instanceof Error ? err : new Error(String(err)));
          setIsLoading(false);
        }
      }
    }

    loadMonaco();

    return () => {
      mounted = false;
      if (timeoutId) {
        clearTimeout(timeoutId);
      }
    };
  }, []);

  // Handle editor mount - store monaco reference and register language
  const handleEditorDidMount = useCallback((_editor: unknown, monaco: Monaco) => {
    monacoRef.current = monaco;

    // Register our custom language if not already registered
    if (!registeredLanguages.has(languageId)) {
      monaco.languages.register({ id: languageId });
      registeredLanguages.add(languageId);
    }
  }, [languageId]);

  // Update completion provider when fields change
  useEffect(() => {
    if (monacoRef.current && availableFields.length > 0) {
      // Dispose previous and create new
      if (disposableRef.current) {
        disposableRef.current.dispose();
      }

      const monaco = monacoRef.current;
      disposableRef.current = monaco.languages.registerCompletionItemProvider(languageId, {
        triggerCharacters: ['.', '{'],
        provideCompletionItems: (model, position) => {
          const textUntilPosition = model.getValueInRange({
            startLineNumber: position.lineNumber,
            startColumn: 1,
            endLineNumber: position.lineNumber,
            endColumn: position.column,
          });

          if (language === 'cel-template') {
            const lastOpenBrace = textUntilPosition.lastIndexOf('{{');
            const lastCloseBrace = textUntilPosition.lastIndexOf('}}');

            if (lastOpenBrace === -1 || lastCloseBrace > lastOpenBrace) {
              return { suggestions: [] };
            }
          }

          const word = model.getWordUntilPosition(position);
          const range = {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: word.startColumn,
            endColumn: word.endColumn,
          };

          const suggestions = availableFields.map((field) => ({
            label: field,
            kind: monaco.languages.CompletionItemKind.Field,
            insertText: field,
            range,
            documentation: `Field path: ${field}`,
          }));

          return { suggestions };
        },
      });
    }

    return () => {
      if (disposableRef.current) {
        disposableRef.current.dispose();
        disposableRef.current = null;
      }
    };
  }, [availableFields, language, languageId]);

  // Handle load error - show error message
  if (loadError) {
    return (
      <div
        className={cn('relative border rounded-md p-3 bg-destructive/10', className)}
        data-testid={testId}
        style={{ height: typeof height === 'number' ? `${height}px` : height }}
      >
        <p className="text-xs text-destructive">
          Monaco editor failed to load: {loadError.message}
        </p>
      </div>
    );
  }

  // Show loading state
  if (isLoading || !Editor) {
    return (
      <div
        className={cn(
          'relative border rounded-md bg-muted animate-pulse',
          className
        )}
        style={{ height: typeof height === 'number' ? `${height}px` : height }}
        data-testid={testId}
      >
        <div className="flex items-center justify-center h-full">
          <span className="text-xs text-muted-foreground">Loading editor...</span>
        </div>
      </div>
    );
  }

  const heightValue = typeof height === 'number' ? `${height}px` : height;

  return (
    <div
      className={cn(
        'border rounded-md',
        error && 'border-destructive',
        className
      )}
      style={{ height: heightValue, position: 'relative' }}
      data-testid={testId}
    >
      <Editor
        height="100%"
        language={languageId}
        value={value}
        onChange={(newValue: string | undefined) => onChange(newValue || '')}
        onMount={handleEditorDidMount}
        loading={<div className="flex items-center justify-center h-full bg-muted"><span className="text-xs text-muted-foreground">Initializing...</span></div>}
        theme={typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: dark)').matches
          ? 'vs-dark'
          : 'vs'}
        options={{
          minimap: { enabled: false },
          lineNumbers: 'off',
          glyphMargin: false,
          folding: false,
          lineDecorationsWidth: 8,
          lineNumbersMinChars: 0,
          scrollBeyondLastLine: false,
          wordWrap: 'on',
          wrappingIndent: 'indent',
          fontSize: 12,
          fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
          readOnly,
          automaticLayout: true,
          padding: {
            top: 8,
            bottom: 8,
          },
          scrollbar: {
            vertical: 'auto',
            horizontal: 'auto',
            verticalScrollbarSize: 8,
            horizontalScrollbarSize: 8,
          },
          suggest: {
            showIcons: true,
            maxVisibleSuggestions: 8,
          },
        }}
      />
    </div>
  );
}
