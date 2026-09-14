/**
 * Utility for extracting field paths from JSON objects
 * Used to populate autocomplete suggestions in CEL editors
 */

/**
 * Check if a key requires bracket notation (contains special characters)
 */
function requiresBracketNotation(key: string): boolean {
  // Keys with dots, slashes, or other special characters need bracket notation
  return /[./\-:]/.test(key) || /^\d/.test(key);
}

/**
 * Build the CEL path for a key, using bracket notation when needed
 */
function buildPath(prefix: string, key: string): string {
  if (requiresBracketNotation(key)) {
    // Use bracket notation with quoted key: prefix["key"] or just ["key"]
    return prefix ? `${prefix}["${key}"]` : `["${key}"]`;
  }
  // Use dot notation: prefix.key or just key
  return prefix ? `${prefix}.${key}` : key;
}

/**
 * Recursively extract all field paths from a JSON object
 *
 * @param obj - The object to extract paths from
 * @param prefix - Current path prefix (for recursion)
 * @param maxDepth - Maximum nesting depth to traverse (default: 5)
 * @returns Array of CEL-compatible field paths (using bracket notation for special keys)
 *
 * @example
 * extractFieldPaths({ objectRef: { name: "foo" }, annotations: { "k8s.io/key": "val" } })
 * // Returns: ["objectRef.name", 'annotations["k8s.io/key"]']
 */
export function extractFieldPaths(
  obj: unknown,
  prefix = '',
  maxDepth = 5
): string[] {
  const paths: string[] = [];

  // Stop if we've reached max depth
  if (maxDepth <= 0) {
    return paths;
  }

  // Handle null/undefined
  if (obj == null) {
    return paths;
  }

  // Handle primitives (add the path itself)
  if (typeof obj !== 'object') {
    if (prefix) {
      paths.push(prefix);
    }
    return paths;
  }

  // Handle arrays - sample the first element only
  if (Array.isArray(obj)) {
    if (obj.length > 0 && prefix) {
      // Add the array path itself
      paths.push(prefix);
      // Recurse into first element
      const firstElement = obj[0];
      if (firstElement != null && typeof firstElement === 'object') {
        const nestedPaths = extractFieldPaths(
          firstElement,
          prefix,
          maxDepth - 1
        );
        paths.push(...nestedPaths);
      }
    }
    return paths;
  }

  // Handle objects
  const objAsRecord = obj as Record<string, unknown>;
  for (const [key, value] of Object.entries(objAsRecord)) {
    const currentPath = buildPath(prefix, key);

    if (value == null) {
      // Add null/undefined fields to the list
      paths.push(currentPath);
    } else if (typeof value === 'object') {
      // Recurse into nested objects/arrays
      const nestedPaths = extractFieldPaths(
        value,
        currentPath,
        maxDepth - 1
      );
      paths.push(...nestedPaths);
    } else {
      // Primitive value - add the path
      paths.push(currentPath);
    }
  }

  return paths;
}

/**
 * Extract field paths from multiple objects and return unique sorted paths
 *
 * @param objects - Array of objects to extract paths from
 * @param maxDepth - Maximum nesting depth to traverse (default: 5)
 * @returns Sorted array of unique field paths
 *
 * @example
 * extractFieldPathsFromMany([
 *   { verb: "create", objectRef: { name: "foo" } },
 *   { verb: "update", objectRef: { namespace: "bar" } }
 * ])
 * // Returns: ["objectRef.name", "objectRef.namespace", "verb"]
 */
export function extractFieldPathsFromMany(
  objects: unknown[],
  maxDepth = 5
): string[] {
  const allPaths = new Set<string>();

  for (const obj of objects) {
    const paths = extractFieldPaths(obj, '', maxDepth);
    paths.forEach(path => allPaths.add(path));
  }

  return Array.from(allPaths).sort();
}
