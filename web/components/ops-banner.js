(function (global) {
    function registerOpsBannerComponent(app) {
        app.component('ops-banner', {
            props: {
                runningJobs: { type: Array, required: true },
                cancelJobFn: { type: Function, default: null }
            },
            data() {
                return { expanded: false, dismissing: null };
            },
            computed: {
                visible() {
                    return this.runningJobs.length > 0;
                },
                primaryJob() {
                    return this.runningJobs[0] || null;
                },
                progressParts() {
                    if (!this.primaryJob || !this.primaryJob.progress) return null;
                    const parts = this.primaryJob.progress.split('/').map(s => s.trim());
                    if (parts.length !== 2) return null;
                    const current = parseInt(parts[0], 10);
                    const total = parseInt(parts[1], 10);
                    if (isNaN(current) || isNaN(total) || total === 0) return null;
                    return { current, total, pct: Math.round((current / total) * 100) };
                }
            },
            methods: {
                cancel(jobID) {
                    if (this.cancelJobFn) {
                        this.dismissing = jobID;
                        this.cancelJobFn(jobID).finally(() => { this.dismissing = null; });
                    }
                }
            },
            template: `
                <Transition
                    enter-active-class="transition-all duration-300 ease-out"
                    enter-from-class="opacity-0 -translate-y-full"
                    enter-to-class="opacity-100 translate-y-0"
                    leave-active-class="transition-all duration-200 ease-in"
                    leave-from-class="opacity-100 translate-y-0"
                    leave-to-class="opacity-0 -translate-y-full"
                >
                <div v-if="visible" class="w-full bg-gray-950 border-b border-curator-500/20 z-40">
                    <!-- single-job compact row -->
                    <div v-if="runningJobs.length === 1 && primaryJob" class="flex items-center gap-3 px-4 py-2">
                        <!-- pulsing indicator -->
                        <span class="relative flex h-2 w-2 shrink-0">
                            <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-curator-500 opacity-60"></span>
                            <span class="relative inline-flex rounded-full h-2 w-2 bg-curator-500"></span>
                        </span>
                        <!-- type label -->
                        <span class="text-xs font-mono text-curator-400 uppercase tracking-wider shrink-0">{{ primaryJob.type }}</span>
                        <!-- progress bar + counter -->
                        <div v-if="progressParts" class="flex items-center gap-2 flex-1 min-w-0">
                            <div class="flex-1 bg-gray-800 rounded-full h-1 min-w-[60px] max-w-xs overflow-hidden">
                                <div
                                    class="h-1 rounded-full bg-curator-500 transition-all duration-300"
                                    :style="{ width: progressParts.pct + '%' }"
                                ></div>
                            </div>
                            <span class="text-xs font-mono text-gray-400 shrink-0 tabular-nums">{{ progressParts.current }}&thinsp;/&thinsp;{{ progressParts.total }}</span>
                        </div>
                        <span v-else class="text-xs font-mono text-gray-500 flex-1">in progress…</span>
                        <!-- cancel button -->
                        <button
                            v-if="cancelJobFn"
                            @click="cancel(primaryJob.id)"
                            :disabled="dismissing === primaryJob.id"
                            class="shrink-0 text-xs font-mono text-gray-600 hover:text-red-400 disabled:opacity-40 transition-colors uppercase tracking-wider"
                        >{{ dismissing === primaryJob.id ? 'cancelling…' : 'cancel' }}</button>
                    </div>

                    <!-- multi-job summary row -->
                    <div v-else>
                        <div class="flex items-center gap-3 px-4 py-2 cursor-pointer" @click="expanded = !expanded">
                            <span class="relative flex h-2 w-2 shrink-0">
                                <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-curator-500 opacity-60"></span>
                                <span class="relative inline-flex rounded-full h-2 w-2 bg-curator-500"></span>
                            </span>
                            <span class="text-xs font-mono text-curator-400 flex-1">{{ runningJobs.length }} operations running</span>
                            <span class="text-xs font-mono text-gray-600">{{ expanded ? '▴' : '▾' }}</span>
                        </div>
                        <!-- expanded per-job list -->
                        <div v-if="expanded" class="border-t border-gray-800 divide-y divide-gray-800/60">
                            <div
                                v-for="job in runningJobs"
                                :key="job.id"
                                class="flex items-center gap-3 px-6 py-1.5"
                            >
                                <span class="text-xs font-mono text-curator-400 uppercase w-24 shrink-0">{{ job.type }}</span>
                                <span class="text-xs font-mono text-gray-500 flex-1">{{ job.progress || 'running…' }}</span>
                                <button
                                    v-if="cancelJobFn"
                                    @click="cancel(job.id)"
                                    :disabled="dismissing === job.id"
                                    class="shrink-0 text-xs font-mono text-gray-600 hover:text-red-400 disabled:opacity-40 transition-colors uppercase"
                                >{{ dismissing === job.id ? '…' : 'cancel' }}</button>
                            </div>
                        </div>
                    </div>
                </div>
                </Transition>
            `
        });
    }

    global.registerOpsBannerComponent = registerOpsBannerComponent;
})(window);
