import { serve } from "std/http/server.ts";

const port = 8080;

const handler = (_request: Request): Response => {
  return new Response("Hello from dynamically built Deno image via Nixpacks + Kaniko!\n", { status: 200 });
};

console.log(`Listening on http://localhost:${port}/`);
serve(handler, { port });
