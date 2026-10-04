// Service entry point.
//
// `./instrumentation.js` is imported statically FIRST so the OTel SDK starts and
// registers its module hooks before any HTTP code is loaded. The server (which
// pulls in express) is then loaded via a dynamic import, guaranteeing it — and
// therefore express/http — is evaluated only after instrumentation is live. This
// ordering holds even without the `--import` preload used by `npm start`.
import './instrumentation.js';

const { start } = await import('./server.js');
start();
