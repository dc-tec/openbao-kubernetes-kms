'use strict';
const { checkRevocation, receipt } = require('./probe.cjs');
checkRevocation(process.env.STATE_directory)
  .then(receipt)
  .catch(() => { console.error('App token post-action revocation was not verified'); process.exitCode = 1; });
