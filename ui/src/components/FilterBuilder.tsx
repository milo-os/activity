import { useState } from 'react';
import type { AuditLogQuerySpec } from '../types';
import { FILTER_FIELDS } from '../types';
import { Button } from '@datum-cloud/datum-ui/button';
import { Card, CardContent, CardHeader } from '@datum-cloud/datum-ui/card';
import { Input } from '@datum-cloud/datum-ui/input';
import { Label } from '@datum-cloud/datum-ui/label';
import { Separator } from '@datum-cloud/datum-ui/separator';
import { Textarea } from '@datum-cloud/datum-ui/textarea';

export interface FilterBuilderProps {
  onFilterChange: (spec: AuditLogQuerySpec) => void;
  initialFilter?: string;
  initialLimit?: number;
  className?: string;
}

/**
 * FilterBuilder component for constructing CEL filter expressions
 */
export function FilterBuilder({
  onFilterChange,
  initialFilter = '',
  initialLimit = 100,
  className = '',
}: FilterBuilderProps) {
  const [filter, setFilter] = useState(initialFilter);
  const [limit, setLimit] = useState(initialLimit);
  const [showHelp, setShowHelp] = useState(false);

  const handleFilterChange = (newFilter: string) => {
    setFilter(newFilter);
    onFilterChange({ filter: newFilter, limit });
  };

  const handleLimitChange = (newLimit: number) => {
    const validLimit = Math.min(Math.max(1, newLimit), 1000);
    setLimit(validLimit);
    onFilterChange({ filter, limit: validLimit });
  };

  const insertExample = (example: string) => {
    const newFilter = filter ? `${filter} && ${example}` : example;
    handleFilterChange(newFilter);
  };

  return (
    <Card className={`mb-6 ${className}`}>
      <CardHeader className="pb-4">
        <div className="flex justify-between items-center">
          <h3 className="m-0 text-2xl font-semibold text-foreground">Build Your Query</h3>
          <Button
            onClick={() => setShowHelp(!showHelp)}
            type="tertiary" theme="outline"
            htmlType="button"
          >
            {showHelp ? 'Hide' : 'Show'} Help
          </Button>
        </div>
      </CardHeader>

      <Separator />

      <CardContent className="pt-6">
        {showHelp && (
          <div className="bg-muted p-6 rounded-lg mb-6 max-h-[400px] overflow-y-auto border border-border">
            <h4 className="text-base font-semibold text-foreground">Available Filter Fields</h4>
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
                      <strong className="text-foreground">Examples:</strong>
                      {field.examples.map((example, idx) => (
                        <div key={idx} className="flex items-center gap-2 my-2">
                          <code className="flex-1 px-2 py-1 bg-background rounded text-sm font-mono">
                            {example}
                          </code>
                          <Button
                            onClick={() => insertExample(example)}
                            type="primary" theme="solid"
                            size="small"
                            className="bg-green-600 hover:bg-green-700 dark:bg-green-700 dark:hover:bg-green-600"
                            htmlType="button"
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

            <div className="mt-4">
              <h4 className="mb-2 text-base font-semibold text-foreground">Common Operators</h4>
              <ul className="m-0 pl-6 text-muted-foreground">
                <li className="my-1"><code className="px-1 py-0.5 bg-background rounded text-sm font-mono">==</code> - Equals</li>
                <li className="my-1"><code className="px-1 py-0.5 bg-background rounded text-sm font-mono">!=</code> - Not equals</li>
                <li className="my-1"><code className="px-1 py-0.5 bg-background rounded text-sm font-mono">&amp;&amp;</code> - And</li>
                <li className="my-1"><code className="px-1 py-0.5 bg-background rounded text-sm font-mono">||</code> - Or</li>
                <li className="my-1"><code className="px-1 py-0.5 bg-background rounded text-sm font-mono">in</code> - In list (e.g., <code className="px-1 py-0.5 bg-background rounded text-sm font-mono">verb in ["create", "delete"]</code>)</li>
                <li className="my-1"><code className="px-1 py-0.5 bg-background rounded text-sm font-mono">.startsWith()</code> - String starts with</li>
                <li className="my-1"><code className="px-1 py-0.5 bg-background rounded text-sm font-mono">.contains()</code> - String contains</li>
                <li className="my-1"><code className="px-1 py-0.5 bg-background rounded text-sm font-mono">timestamp()</code> - Parse timestamp (e.g., <code className="px-1 py-0.5 bg-background rounded text-sm font-mono">timestamp("2024-01-01T00:00:00Z")</code>)</li>
              </ul>
            </div>
          </div>
        )}

        <div className="mb-4">
          <Label htmlFor="filter-input" className="block mb-2">
            CEL Filter Expression
          </Label>
          <Textarea
            id="filter-input"
            value={filter}
            onChange={(e) => handleFilterChange(e.target.value)}
            placeholder='e.g., verb == "delete" && ns == "production"'
            rows={4}
            className="font-mono text-sm resize-y"
          />
        </div>

        <div className="mb-4">
          <Label htmlFor="limit-input" className="block mb-2">
            Result Limit (max 1,000)
          </Label>
          <Input
            id="limit-input"
            type="number"
            value={limit}
            onChange={(e) => handleLimitChange(parseInt(e.target.value) || 100)}
            min={1}
            max={1000}
            className="w-44"
          />
        </div>

        <Separator className="my-6" />

        <div>
          <h4 className="mb-3 text-base font-semibold text-foreground">Quick Filters</h4>
          <div className="flex flex-wrap gap-2">
            <Button
              onClick={() => insertExample('verb == "delete"')}
              htmlType="button"
              type="tertiary" theme="outline"
            >
              Delete Operations
            </Button>
            <Button
              onClick={() => insertExample('resource == "secrets"')}
              htmlType="button"
              type="tertiary" theme="outline"
            >
              Secret Access
            </Button>
            <Button
              onClick={() => insertExample('user.startsWith("system:")')}
              htmlType="button"
              type="tertiary" theme="outline"
            >
              System Users
            </Button>
            <Button
              onClick={() => insertExample('stage == "ResponseComplete"')}
              htmlType="button"
              type="tertiary" theme="outline"
            >
              Completed Requests
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
