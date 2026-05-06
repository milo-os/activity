import resolve from '@rollup/plugin-node-resolve';
import commonjs from '@rollup/plugin-commonjs';
import typescript from '@rollup/plugin-typescript';
import peerDepsExternal from 'rollup-plugin-peer-deps-external';

// We don't ship CSS — the host app's Tailwind compiles utility classes
// directly from this package's dist via @source. No PostCSS pipeline
// needed.

export default {
  input: 'src/index.ts',
  output: [
    {
      file: 'dist/index.js',
      format: 'cjs',
      sourcemap: true,
      exports: 'named',
    },
    {
      file: 'dist/index.esm.js',
      format: 'esm',
      sourcemap: true,
    },
  ],
  plugins: [
    peerDepsExternal(),
    resolve({
      extensions: ['.ts', '.tsx', '.js', '.jsx'],
    }),
    commonjs({
      include: /node_modules/,
      // Don't try to convert React to CommonJS
      ignore: ['react', 'react-dom', 'react/jsx-runtime'],
    }),
    typescript({
      tsconfig: './tsconfig.json',
      declaration: true,
      declarationDir: 'dist',
      noEmitOnError: false,
    }),
  ],
  external: [
    'react',
    'react-dom',
    'react/jsx-runtime',
    /^react\//,
    /^react-dom\//,
    /^@radix-ui\//,
    // Externalize @datum-cloud/datum-ui (and its subpath imports) so the
    // consumer brings its own pinned copy. Avoids duplicating primitives
    // and prevents CSS conflicts with the host's datum-ui styles.
    /^@datum-cloud\/datum-ui($|\/)/,
  ],
};
