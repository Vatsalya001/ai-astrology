/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,

  // @ayana/types ships TypeScript source rather than build output,
  // so Next has to compile it alongside the app.
  transpilePackages: ['@ayana/types'],

  // Fail the build on type errors. Letting these through "temporarily"
  // is how a codebase ends up with hundreds of them.
  typescript: { ignoreBuildErrors: false },

  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          { key: 'X-Content-Type-Options', value: 'nosniff' },
          { key: 'X-Frame-Options', value: 'DENY' },
          { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
          {
            key: 'Permissions-Policy',
            value: 'camera=(), microphone=(), geolocation=()',
          },
        ],
      },
    ]
  },
}

export default nextConfig
