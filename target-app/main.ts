const port = 8080;

const handler = (_request: Request): Response => {
  return new Response("Hello from dynamically built Deno image via Nixpacks + BuildKit!\n", { status: 200 });
};

console.log(`Listening on http://localhost:${port}/`);
Deno.serve({ port }, handler);
