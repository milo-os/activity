import { useState, useRef, useEffect } from 'react';
import type { AuditLogQuerySpec } from '../types';
import { FILTER_FIELDS } from '../types';
import { Input } from '@datum-cloud/datum-ui/input';
import { Textarea } from '@datum-cloud/datum-ui/textarea';
import { Button } from '@datum-cloud/datum-ui/button';
import { Label } from '@datum-cloud/datum-ui/label';
import { Card, CardHeader, CardContent } from '@datum-cloud/datum-ui/card';
import { Badge } from './ui/badge';

export interface FilterBuilderWithAutocompleteProps {
  onFilterChange: (spec: AuditLogQuerySpec) => void;
  initialFilter?: string;
  initialLimit?: number;
  className?: string;
}

interface Suggestion {
  text: string;
  description: string;
  type: 'field' | 'operator' | 'value' | 'function';
}

const OPERATORS: Suggestion[] = [
  { text: '==', description: 'Equals', type: 'operator' },
  { text: '!=', description: 'Not equals', type: 'operator' },
  { text: '>', description: 'Greater than', type: 'operator' },
  { text: '>=', description: 'Greater than or equal', type: 'operator' },
  { text: '<', description: 'Less than', type: 'operator' },
  { text: '<=', description: 'Less than or equal', type: 'operator' },
  { text: 'in', description: 'In array', type: 'operator' },
  { text: '&&', description: 'Logical AND', type: 'operator' },
  { text: '||', description: 'Logical OR', type: 'operator' },
];

const FUNCTIONS: Suggestion[] = [
  { text: '.startsWith(', description: 'String starts with', type: 'function' },
  { text: '.contains(', description: 'String contains', type: 'function' },
  { text: '.endsWith(', description: 'String ends with', type: 'function' },
  { text: '.matches(', description: 'Regex match', type: 'function' },
  { text: 'timestamp(', description: 'Parse timestamp', type: 'function' },
];

const COMMON_VALUES: Record<string, Suggestion[]> = {
  verb: [
    { text: '"get"', description: 'GET requests', type: 'value' },
    { text: '"list"', description: 'LIST requests', type: 'value' },
    { text: '"create"', description: 'CREATE requests', type: 'value' },
    { text: '"update"', description: 'UPDATE requests', type: 'value' },
    { text: '"patch"', description: 'PATCH requests', type: 'value' },
    { text: '"delete"', description: 'DELETE requests', type: 'value' },
    { text: '"watch"', description: 'WATCH requests', type: 'value' },
  ],
  'objectRef.resource': [
    { text: '"pods"', description: 'Pods', type: 'value' },
    { text: '"deployments"', description: 'Deployments', type: 'value' },
    { text: '"services"', description: 'Services', type: 'value' },
    { text: '"secrets"', description: 'Secrets', type: 'value' },
    { text: '"configmaps"', description: 'ConfigMaps', type: 'value' },
    { text: '"namespaces"', description: 'Namespaces', type: 'value' },
    { text: '"replicasets"', description: 'ReplicaSets', type: 'value' },
    { text: '"daemonsets"', description: 'DaemonSets', type: 'value' },
    { text: '"statefulsets"', description: 'StatefulSets', type: 'value' },
  ],
  stage: [
    { text: '"RequestReceived"', description: 'Request received', type: 'value' },
    { text: '"ResponseStarted"', description: 'Response started', type: 'value' },
    { text: '"ResponseComplete"', description: 'Response complete', type: 'value' },
    { text: '"Panic"', description: 'Panic occurred', type: 'value' },
  ],
  level: [
    { text: '"Metadata"', description: 'Metadata level', type: 'value' },
    { text: '"Request"', description: 'Request level', type: 'value' },
    { text: '"RequestResponse"', description: 'Request and Response', type: 'value' },
  ],
};

/**
 * Enhanced FilterBuilder with autocomplete suggestions for CEL expressions
 */
export function FilterBuilderWithAutocomplete({
  onFilterChange,
  initialFilter = '',
  initialLimit = 100,
  className = '',
}: FilterBuilderWithAutocompleteProps) {
  const [filter, setFilter] = useState(initialFilter);
  const [limit, setLimit] = useState(initialLimit);
  const [showHelp, setShowHelp] = useState(false);
  const [showShortcuts, setShowShortcuts] = useState(false);
  const [suggestions, setSuggestions] = useState<Suggestion[]>([]);
  const [showSuggestions, setShowSuggestions] = useState(false);
  const [selectedSuggestion, setSelectedSuggestion] = useState(0);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleFilterChange = (newFilter: string) => {
    setFilter(newFilter);
    onFilterChange({ filter: newFilter, limit });
    updateSuggestions(newFilter);
  };

  const handleLimitChange = (newLimit: number) => {
    const validLimit = Math.min(Math.max(1, newLimit), 1000);
    setLimit(validLimit);
    onFilterChange({ filter, limit: validLimit });
  };

  const updateSuggestions = (text: string) => {
    const cursorPos = textareaRef.current?.selectionStart || text.length;
    const textBeforeCursor = text.substring(0, cursorPos);
    const lastWord = textBeforeCursor.split(/[\s()[\]]/g).pop() || '';

    let newSuggestions: Suggestion[] = [];

    // Check if we just finished a complete expression (e.g., verb == "get")
    // Suggest logical operators to continue
    const completedValuePattern = /["'][^"']*["']\s*$/;
    const completedNumberPattern = /\d+\s*$/;
    const afterLogicalOp = /(\&\&|\|\|)\s*$/;

    if ((completedValuePattern.test(textBeforeCursor) || completedNumberPattern.test(textBeforeCursor)) && !afterLogicalOp.test(textBeforeCursor)) {
      // After a complete value, suggest logical operators
      newSuggestions.push(
        { text: ' && ', description: 'Logical AND - add another condition', type: 'operator' },
        { text: ' || ', description: 'Logical OR - add alternative condition', type: 'operator' }
      );
    }

    // After && or || suggest fields
    if (afterLogicalOp.test(textBeforeCursor)) {
      const fieldSuggestions = FILTER_FIELDS.map(f => ({
        text: f.name,
        description: f.description,
        type: 'field' as const,
      }));
      newSuggestions.push(...fieldSuggestions);
    }

    // Field name suggestions when typing
    if (lastWord && !lastWord.includes('==') && !lastWord.includes('!=') && !lastWord.includes('&&') && !lastWord.includes('||')) {
      const fieldSuggestions = FILTER_FIELDS
        .filter(f => f.name.toLowerCase().startsWith(lastWord.toLowerCase()))
        .map(f => ({
          text: f.name,
          description: f.description,
          type: 'field' as const,
        }));
      if (fieldSuggestions.length > 0) {
        newSuggestions = fieldSuggestions; // Replace suggestions with field matches
      }
    }

    // Operator suggestions after field name
    const lastToken = textBeforeCursor.trim().split(/\s+/).pop() || '';
    if (FILTER_FIELDS.some(f => lastToken === f.name)) {
      newSuggestions.push(...OPERATORS.filter(op => !['&&', '||'].includes(op.text)));
    }

    // Function suggestions when typing dot
    if (lastWord.startsWith('.') || textBeforeCursor.endsWith('.')) {
      newSuggestions.push(...FUNCTIONS);
    }

    // Value suggestions based on field
    for (const [fieldName, values] of Object.entries(COMMON_VALUES)) {
      const pattern = new RegExp(`${fieldName}\\s*[!=<>]+\\s*$`);
      if (pattern.test(textBeforeCursor)) {
        newSuggestions.push(...values);
      }
    }

    setSuggestions(newSuggestions);
    setShowSuggestions(newSuggestions.length > 0);
    setSelectedSuggestion(0);
  };

  const insertSuggestion = (suggestion: Suggestion) => {
    const textarea = textareaRef.current;
    if (!textarea) return;

    const cursorPos = textarea.selectionStart;
    const textBefore = filter.substring(0, cursorPos);
    const textAfter = filter.substring(cursorPos);

    // Find the last word to replace
    const lastSpaceIndex = Math.max(
      textBefore.lastIndexOf(' '),
      textBefore.lastIndexOf('('),
      textBefore.lastIndexOf('['),
      textBefore.lastIndexOf('.'),
      0
    );

    const beforeWord = textBefore.substring(0, lastSpaceIndex === 0 ? 0 : lastSpaceIndex + 1);
    const newText = beforeWord + suggestion.text + textAfter;

    setFilter(newText);
    onFilterChange({ filter: newText, limit });
    setShowSuggestions(false);

    // Set cursor position after inserted text
    setTimeout(() => {
      const newPos = beforeWord.length + suggestion.text.length;
      textarea.setSelectionRange(newPos, newPos);
      textarea.focus();
    }, 0);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Ctrl+Space to manually trigger autocomplete
    if (e.key === ' ' && e.ctrlKey) {
      e.preventDefault();
      updateSuggestions(filter);
      return;
    }

    if (!showSuggestions) return;

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        setSelectedSuggestion(prev =>
          prev < suggestions.length - 1 ? prev + 1 : prev
        );
        break;
      case 'ArrowUp':
        e.preventDefault();
        setSelectedSuggestion(prev => prev > 0 ? prev - 1 : prev);
        break;
      case 'Enter':
      case 'Tab':
        if (suggestions.length > 0) {
          e.preventDefault();
          insertSuggestion(suggestions[selectedSuggestion]);
        }
        break;
      case 'Escape':
        setShowSuggestions(false);
        break;
    }
  };

  const insertExample = (example: string) => {
    const newFilter = filter ? `${filter} && ${example}` : example;
    handleFilterChange(newFilter);
  };

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (textareaRef.current && !textareaRef.current.contains(e.target as Node)) {
        setShowSuggestions(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const getBadgeVariant = (type: string): 'default' | 'secondary' | 'success' | 'warning' | 'outline' => {
    switch (type) {
      case 'field':
        return 'default';
      case 'operator':
        return 'secondary';
      case 'value':
        return 'success';
      case 'function':
        return 'warning';
      default:
        return 'outline';
    }
  };

  return (
    <Card className={`p-8 rounded-xl mb-6 ${className}`}>
      <CardHeader className="flex flex-row justify-between items-center p-0 mb-6 pb-4 border-b border-border">
        <h3 className="m-0 text-2xl font-semibold text-foreground">Search Audit Logs</h3>
        <div className="flex gap-2">
          <Button
            onClick={() => setShowShortcuts(!showShortcuts)}
            type="tertiary" theme="outline"
            size="small"
            htmlType="button"
            title="View keyboard shortcuts"
          >
            Shortcuts
          </Button>
          <Button
            onClick={() => setShowHelp(!showHelp)}
            type="tertiary" theme="outline"
            size="small"
            htmlType="button"
            title="View available filter fields and examples"
          >
            Field Guide
          </Button>
        </div>
      </CardHeader>

      <CardContent className="p-0">
        {showShortcuts && (
          <div className="bg-blue-50 border border-blue-500 dark:bg-blue-950/50 dark:border-blue-700 p-6 rounded-lg mb-6 max-h-96 overflow-y-auto">
            <h4 className="mt-0 mb-4 text-lg font-semibold text-foreground">Keyboard Shortcuts</h4>
            <div className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-3 items-start">
              <kbd className="px-2 py-1 bg-background border border-input rounded font-mono text-sm">Ctrl+Space</kbd>
              <span>Trigger autocomplete suggestions at cursor position</span>

              <kbd className="px-2 py-1 bg-background border border-input rounded font-mono text-sm">&uarr; / &darr;</kbd>
              <span>Navigate through autocomplete suggestions</span>

              <kbd className="px-2 py-1 bg-background border border-input rounded font-mono text-sm">Tab</kbd>
              <span>Accept the selected suggestion</span>

              <kbd className="px-2 py-1 bg-background border border-input rounded font-mono text-sm">Enter</kbd>
              <span>Accept the selected suggestion</span>

              <kbd className="px-2 py-1 bg-background border border-input rounded font-mono text-sm">Esc</kbd>
              <span>Close autocomplete suggestions</span>
            </div>

            <div className="mt-4 p-3 bg-background rounded text-sm">
              <strong>Pro Tips:</strong>
              <ul className="mt-2 mb-0 pl-6">
                <li>Autocomplete appears automatically as you type</li>
                <li>After completing a value (e.g., <code className="px-1 py-0.5 bg-muted rounded text-sm">"get"</code>), suggestions show logical operators (<code className="px-1 py-0.5 bg-muted rounded text-sm">&&</code>, <code className="px-1 py-0.5 bg-muted rounded text-sm">||</code>)</li>
                <li>After typing <code className="px-1 py-0.5 bg-muted rounded text-sm">&&</code> or <code className="px-1 py-0.5 bg-muted rounded text-sm">||</code>, all field names are suggested</li>
                <li>Type <code className="px-1 py-0.5 bg-muted rounded text-sm">.</code> after a field name to see available string functions</li>
              </ul>
            </div>
          </div>
        )}

        {showHelp && (
          <div className="bg-muted p-6 rounded-lg mb-6 max-h-96 overflow-y-auto">
            <h4 className="mt-0 mb-4 text-lg font-semibold text-foreground">Available Fields & Examples</h4>
            <p className="mb-4 text-muted-foreground text-sm">
              Click "Insert" to add an example to your query
            </p>
            <div className="mt-4">
              {FILTER_FIELDS.map((field) => (
                <div key={field.name} className="mb-6 pb-4 border-b border-border last:border-b-0">
                  <div className="flex justify-between items-center mb-2">
                    <strong className="text-foreground">{field.name}</strong>
                    <span className="text-sm text-muted-foreground italic">{field.type}</span>
                  </div>
                  <p className="my-2 text-muted-foreground">{field.description}</p>
                  {field.examples && field.examples.length > 0 && (
                    <div className="mt-2">
                      <strong className="text-sm text-foreground">Examples:</strong>
                      {field.examples.map((example, idx) => (
                        <div key={idx} className="flex items-center gap-2 my-2">
                          <code className="flex-1 px-2 py-1 bg-background rounded text-sm font-mono">{example}</code>
                          <Button
                            onClick={() => insertExample(example)}
                            type="primary" theme="solid"
                            size="small"
                            htmlType="button"
                            className="bg-green-600 hover:bg-green-700 dark:bg-green-700 dark:hover:bg-green-600"
                          >
                            Insert
                          </Button>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        <div className="relative mb-4">
          <Label htmlFor="filter-input" className="block mb-2">
            <strong className="text-foreground">Filter Expression</strong>
            <span className="ml-2 text-sm text-muted-foreground font-normal">
              Press Ctrl+Space for suggestions
            </span>
          </Label>
          <p className="m-0 mb-3 text-sm text-muted-foreground">
            Use autocomplete to build your query, or type field names directly (e.g., verb, objectRef.resource)
          </p>
          <Textarea
            ref={textareaRef}
            id="filter-input"
            value={filter}
            onChange={(e) => handleFilterChange(e.target.value)}
            onKeyDown={handleKeyDown}
            onFocus={() => updateSuggestions(filter)}
            placeholder='Example: verb == "delete" && objectRef.namespace == "production"'
            rows={4}
            className="font-mono text-sm bg-muted resize-y focus:bg-background"
          />

          {showSuggestions && suggestions.length > 0 && (
            <div className="absolute top-full left-0 right-0 max-h-72 overflow-y-auto bg-background border border-input border-t-0 rounded-b shadow-md z-50 -mt-1">
              {suggestions.map((suggestion, idx) => (
                <div
                  key={idx}
                  className={`px-4 py-3 cursor-pointer flex items-center gap-3 border-b border-border transition-colors ${
                    idx === selectedSuggestion ? 'bg-pink-100' : 'hover:bg-muted'
                  }`}
                  onClick={() => insertSuggestion(suggestion)}
                  onMouseEnter={() => setSelectedSuggestion(idx)}
                >
                  <span className="font-mono font-semibold text-foreground min-w-[120px]">{suggestion.text}</span>
                  <span className="flex-1 text-muted-foreground text-sm">{suggestion.description}</span>
                  <Badge variant={getBadgeVariant(suggestion.type)} className="uppercase">
                    {suggestion.type}
                  </Badge>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="mb-4">
          <Label htmlFor="limit-input" className="block mb-2">
            <strong className="text-foreground">Number of results</strong>
            <span className="ml-2 text-sm text-muted-foreground font-normal">
              (1 to 1,000 events)
            </span>
          </Label>
          <Input
            id="limit-input"
            type="number"
            value={limit}
            onChange={(e) => handleLimitChange(parseInt(e.target.value) || 100)}
            min={1}
            max={1000}
            className="w-44 bg-muted focus:bg-background"
          />
        </div>
      </CardContent>
    </Card>
  );
}
