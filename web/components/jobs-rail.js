(function (global) {
    function registerJobsRailComponent(app) {
        app.component('jobs-rail', {
            props: {
                runningCount: { type: Number, required: true },
                failedCount: { type: Number, required: true },
                cancelledCount: { type: Number, required: true },
                batchStats: {
                    type: Object,
                    required: true,
                    default: function () {
                        return { total: 0, completed: 0, running: 0 };
                    }
                },
                railRunningJobs: { type: Array, required: true },
                latestTerminalJob: { type: Object, default: null },
                formatRelativeFn: { type: Function, required: true },
                jobSummaryLineFn: { type: Function, required: true },
                subtitle: { type: String, default: '' },
                linkHref: { type: String, default: '' },
                linkText: { type: String, default: '' },
                rightLabel: { type: String, default: '' },
                className: { type: String, default: '' }
            },
            template: `
                <section :class="className || 'bg-gray-900 border border-gray-800 rounded-lg shadow-lg p-4'">
                    <div class="flex items-start justify-between gap-4 flex-wrap">
                        <div>
                            <h2 class="text-sm font-mono font-bold text-curator-500 uppercase">async jobs</h2>
                            <p class="text-xs font-mono text-gray-500 mt-1">{{ subtitle }}</p>
                        </div>
                        <a
                            v-if="linkHref"
                            :href="linkHref"
                            class="text-xs font-mono text-gray-400 hover:text-curator-500 transition-colors uppercase"
                        >{{ linkText || 'open jobs ->' }}</a>
                        <span v-else-if="rightLabel" class="text-xs font-mono text-gray-600 uppercase">{{ rightLabel }}</span>
                    </div>

                    <div class="flex flex-wrap gap-2 mt-3">
                        <span class="px-2.5 py-1 rounded border border-curator-500/30 bg-curator-500/10 text-curator-400 text-xs font-mono">
                            {{ runningCount }} running
                        </span>
                        <span class="px-2.5 py-1 rounded border border-red-500/30 bg-red-500/10 text-red-300 text-xs font-mono">
                            {{ failedCount }} failed
                        </span>
                        <span class="px-2.5 py-1 rounded border border-amber-500/30 bg-amber-500/10 text-amber-300 text-xs font-mono">
                            {{ cancelledCount }} cancelled
                        </span>
                        <span
                            v-if="batchStats && batchStats.total > 0"
                            class="px-2.5 py-1 rounded border border-emerald-500/30 bg-emerald-500/10 text-emerald-300 text-xs font-mono"
                        >
                            {{ batchStats.completed }}/{{ batchStats.total }} complete
                        </span>
                    </div>

                    <div class="grid grid-cols-1 xl:grid-cols-2 gap-3 mt-4">
                        <div class="bg-gray-950/60 border border-gray-800 rounded-lg p-3">
                            <div class="flex items-center justify-between gap-2 mb-2">
                                <span class="text-xs font-mono font-bold text-gray-300 uppercase">running now</span>
                                <span class="text-xs font-mono text-gray-600">{{ railRunningJobs.length }}</span>
                            </div>
                            <div v-if="railRunningJobs.length === 0" class="text-xs font-mono text-gray-600">no live jobs</div>
                            <ul v-else class="space-y-2">
                                <li v-for="job in railRunningJobs" :key="job.id" class="flex items-start gap-3">
                                    <span class="mt-1 w-2 h-2 rounded-full bg-curator-500 animate-pulse shrink-0"></span>
                                    <div class="min-w-0 flex-1">
                                        <div class="flex items-center justify-between gap-2">
                                            <span class="text-xs font-mono text-gray-300 truncate">{{ job.type }} #{{ job.id }}</span>
                                            <span class="text-xs font-mono text-gray-600 shrink-0">{{ formatRelativeFn(job.started_at) }}</span>
                                        </div>
                                        <p class="text-xs font-mono text-curator-500/80 truncate">{{ jobSummaryLineFn(job) }}</p>
                                    </div>
                                </li>
                            </ul>
                        </div>

                        <div class="bg-gray-950/60 border border-gray-800 rounded-lg p-3">
                            <div class="flex items-center justify-between gap-2 mb-2">
                                <span class="text-xs font-mono font-bold text-gray-300 uppercase">latest terminal</span>
                                <span v-if="latestTerminalJob" class="text-xs font-mono text-gray-600">{{ formatRelativeFn(latestTerminalJob.completed_at || latestTerminalJob.started_at) }}</span>
                            </div>
                            <div v-if="!latestTerminalJob" class="text-xs font-mono text-gray-600">no terminal jobs yet</div>
                            <div v-else class="space-y-1">
                                <div class="flex items-center gap-2">
                                    <span :class="[
                                        'w-2 h-2 rounded-full shrink-0',
                                        latestTerminalJob.status === 'completed' ? 'bg-emerald-500' :
                                        latestTerminalJob.status === 'cancelled' ? 'bg-amber-500' : 'bg-red-500'
                                    ]"></span>
                                    <span class="text-xs font-mono text-gray-300">{{ latestTerminalJob.type }} #{{ latestTerminalJob.id }}</span>
                                    <span class="text-xs font-mono uppercase text-gray-600">{{ latestTerminalJob.status }}</span>
                                </div>
                                <p class="text-xs font-mono text-gray-500">{{ jobSummaryLineFn(latestTerminalJob) }}</p>
                            </div>
                        </div>
                    </div>
                </section>
            `
        });
    }

    global.registerJobsRailComponent = registerJobsRailComponent;
})(window);