(function(exports) {
/**
 * resolveRace returns a promise that resolves when the first promise
 * resolves and rejects only after every promise has rejected.
 * @param {promise[]} promises a list of promises.
 * @return {promse} a promise that resolves when any of the inputs resolve, or
 * when all of the inputs reject.
 */
function resolveRace(promises) {
    var resolve, reject;
    var promise = new Promise(function(pResolve, pReject) {
        resolve = pResolve;
        reject = pReject;
    });
    var numRejects = 0;
    for (var i in promises) {
        promises[i].then(resolve, function(reason) {
            numRejects++;
            if (numRejects == promises.length) {
                reject(reason);
            }
        });
    }
    return promise;
};

exports.resolveRace = resolveRace;
})(window);